package message

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"go.airlance.org/server/internal/domain/identity"
	domain "go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/profile"
)

type NewChat struct {
	ClientID string
	Kind     domain.Kind
	Title    string
	About    string
	Link     string
	People   []string
}

func (s *Service) Create(ctx context.Context, principal identity.Principal, next NewChat) (domain.Chat, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return domain.Chat{}, identity.ErrForbidden
	}

	if len(next.People) > domain.MaxInitialPeople {
		return domain.Chat{}, domain.ErrInvalidPeer
	}

	draft, err := s.draft(principal.Subject, next)
	if err != nil {
		return domain.Chat{}, err
	}

	people, err := s.reachable(ctx, principal.Subject, next.People)
	if err != nil {
		return domain.Chat{}, err
	}

	id, woken, err := s.repository.Create(ctx, draft, people)
	if err != nil {
		return domain.Chat{}, fmt.Errorf("create chat: %w", err)
	}

	s.wake(woken)

	return s.chatOf(ctx, id, principal.Subject)
}

func (s *Service) draft(owner string, next NewChat) (domain.NewChat, error) {
	id, err := newChatID()
	if err != nil {
		return domain.NewChat{}, err
	}

	draft, err := domain.NewChat{
		ID:        id,
		CreatedBy: owner,
		ClientID:  next.ClientID,
		Kind:      next.Kind,
		Title:     next.Title,
		About:     next.About,
		Link:      next.Link,
		CreatedAt: s.now(),
	}.Checked()
	if err != nil {
		return domain.NewChat{}, fmt.Errorf("check chat: %w", err)
	}

	if draft.Link == "" {
		draft.InviteToken, err = newChatID()
		if err != nil {
			return domain.NewChat{}, err
		}
	}

	return draft, nil
}

func (s *Service) reachable(ctx context.Context, owner string, people []string) ([]string, error) {
	found := make([]string, 0, len(people))

	for _, person := range people {
		if person == "" || person == owner || slices.Contains(found, person) {
			continue
		}

		err := s.checkInvite(ctx, owner, person)
		if errors.Is(err, domain.ErrPeerNotFound) || errors.Is(err, domain.ErrRestricted) {
			continue
		}

		if err != nil {
			return nil, err
		}

		found = append(found, person)
	}

	return found, nil
}

func (s *Service) checkInvite(ctx context.Context, from, to string) error {
	_, err := s.profiles.Get(ctx, to)
	if errors.Is(err, profile.ErrNotFound) {
		return domain.ErrPeerNotFound
	}

	if err != nil {
		return fmt.Errorf("get person: %w", err)
	}

	if err := s.guard.CheckOpen(ctx, from, to); err != nil {
		return fmt.Errorf("check invite: %w", err)
	}

	return nil
}

func (s *Service) Join(ctx context.Context, principal identity.Principal, code string) (domain.Chat, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return domain.Chat{}, identity.ErrForbidden
	}

	if code == "" {
		return domain.Chat{}, domain.ErrChatNotFound
	}

	id, woken, err := s.repository.Join(ctx, code, principal.Subject, s.now())
	if err != nil {
		return domain.Chat{}, fmt.Errorf("join chat: %w", err)
	}

	s.wake(woken)

	return s.chatOf(ctx, id, principal.Subject)
}

func (s *Service) Leave(ctx context.Context, principal identity.Principal, chatID string) error {
	if !principal.HasScope(domain.ScopeWrite) {
		return identity.ErrForbidden
	}

	woken, err := s.repository.RemoveMember(ctx, chatID, principal.Subject, principal.Subject, false, s.now())
	if err != nil {
		return fmt.Errorf("leave chat: %w", err)
	}

	s.wake(woken)

	return nil
}

func (s *Service) AddMember(
	ctx context.Context,
	principal identity.Principal,
	chatID, subject string,
	role domain.Role,
	rights domain.Rights,
) ([]domain.Member, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return nil, identity.ErrForbidden
	}

	if subject == "" || (role != domain.RoleMember && role != domain.RoleAdmin) {
		return nil, domain.ErrInvalidPeer
	}

	isMember, err := s.repository.IsMember(ctx, chatID, subject)
	if err != nil {
		return nil, fmt.Errorf("check member: %w", err)
	}

	if !isMember {
		if err := s.checkInvite(ctx, principal.Subject, subject); err != nil {
			return nil, err
		}
	}

	woken, err := s.repository.AddMember(ctx, chatID, principal.Subject, subject, role, rights, s.now())
	if err != nil {
		return nil, fmt.Errorf("add member: %w", err)
	}

	s.wake(woken)

	return s.Members(ctx, principal, chatID)
}

func (s *Service) RemoveMember(
	ctx context.Context,
	principal identity.Principal,
	chatID, subject string,
	ban bool,
) ([]domain.Member, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return nil, identity.ErrForbidden
	}

	woken, err := s.repository.RemoveMember(ctx, chatID, principal.Subject, subject, ban, s.now())
	if err != nil {
		return nil, fmt.Errorf("remove member: %w", err)
	}

	s.wake(woken)

	return s.Members(ctx, principal, chatID)
}

func (s *Service) SetMemberRights(
	ctx context.Context,
	principal identity.Principal,
	chatID string,
	rights domain.Rights,
) (domain.Chat, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return domain.Chat{}, identity.ErrForbidden
	}

	woken, err := s.repository.SetMemberRights(ctx, chatID, principal.Subject, rights)
	if err != nil {
		return domain.Chat{}, fmt.Errorf("set member rights: %w", err)
	}

	s.wake(woken)

	return s.chatOf(ctx, chatID, principal.Subject)
}

func (s *Service) SetInfo(
	ctx context.Context,
	principal identity.Principal,
	chatID string,
	info domain.ChatInfo,
) (domain.Chat, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return domain.Chat{}, identity.ErrForbidden
	}

	checked, err := info.Checked()
	if err != nil {
		return domain.Chat{}, fmt.Errorf("check info: %w", err)
	}

	token, err := newChatID()
	if err != nil {
		return domain.Chat{}, err
	}

	woken, err := s.repository.SetInfo(ctx, chatID, principal.Subject, checked, token)
	if err != nil {
		return domain.Chat{}, fmt.Errorf("set info: %w", err)
	}

	s.wake(woken)

	return s.chatOf(ctx, chatID, principal.Subject)
}

func (s *Service) SetRules(
	ctx context.Context,
	principal identity.Principal,
	chatID string,
	rules domain.ChatRules,
) (domain.Chat, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return domain.Chat{}, identity.ErrForbidden
	}

	checked, err := rules.Checked()
	if err != nil {
		return domain.Chat{}, fmt.Errorf("check rules: %w", err)
	}

	woken, err := s.repository.SetRules(ctx, chatID, principal.Subject, checked)
	if err != nil {
		return domain.Chat{}, fmt.Errorf("set rules: %w", err)
	}

	s.wake(woken)

	return s.chatOf(ctx, chatID, principal.Subject)
}

func (s *Service) Bans(ctx context.Context, principal identity.Principal, chatID string) ([]domain.Member, error) {
	if !principal.HasScope(domain.ScopeRead) {
		return nil, identity.ErrForbidden
	}

	bans, err := s.repository.Bans(ctx, chatID, principal.Subject, domain.MaxMembersPage)
	if err != nil {
		return nil, fmt.Errorf("read bans: %w", err)
	}

	return bans, nil
}

func (s *Service) Unban(
	ctx context.Context,
	principal identity.Principal,
	chatID, subject string,
) ([]domain.Member, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return nil, identity.ErrForbidden
	}

	if err := s.repository.Unban(ctx, chatID, principal.Subject, subject); err != nil {
		return nil, fmt.Errorf("unban: %w", err)
	}

	return s.Bans(ctx, principal, chatID)
}

func (s *Service) Members(
	ctx context.Context,
	principal identity.Principal,
	chatID string,
) ([]domain.Member, error) {
	if !principal.HasScope(domain.ScopeRead) {
		return nil, identity.ErrForbidden
	}

	members, err := s.repository.Members(ctx, chatID, principal.Subject, domain.MaxMembersPage)
	if err != nil {
		return nil, fmt.Errorf("read members: %w", err)
	}

	return members, nil
}

func (s *Service) ResetInvite(ctx context.Context, principal identity.Principal, chatID string) (domain.Chat, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return domain.Chat{}, identity.ErrForbidden
	}

	token, err := newChatID()
	if err != nil {
		return domain.Chat{}, err
	}

	if err := s.repository.ResetInvite(ctx, chatID, principal.Subject, token); err != nil {
		return domain.Chat{}, fmt.Errorf("reset invite: %w", err)
	}

	return s.chatOf(ctx, chatID, principal.Subject)
}

func (s *Service) SetPinned(
	ctx context.Context,
	principal identity.Principal,
	chatID string,
	isPinned bool,
) (domain.Chat, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return domain.Chat{}, identity.ErrForbidden
	}

	if err := s.repository.SetPinned(ctx, chatID, principal.Subject, isPinned); err != nil {
		return domain.Chat{}, fmt.Errorf("pin chat: %w", err)
	}

	s.notifier.Notify(principal.Subject)

	return s.chatOf(ctx, chatID, principal.Subject)
}

func (s *Service) SetMuted(
	ctx context.Context,
	principal identity.Principal,
	chatID string,
	isMuted bool,
) (domain.Chat, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return domain.Chat{}, identity.ErrForbidden
	}

	if err := s.repository.SetMuted(ctx, chatID, principal.Subject, isMuted); err != nil {
		return domain.Chat{}, fmt.Errorf("mute chat: %w", err)
	}

	s.notifier.Notify(principal.Subject)

	return s.chatOf(ctx, chatID, principal.Subject)
}

func (s *Service) chatOf(ctx context.Context, id, subject string) (domain.Chat, error) {
	chat, err := s.repository.Chat(ctx, id, subject)
	if err != nil {
		return domain.Chat{}, fmt.Errorf("read chat: %w", err)
	}

	return chat, nil
}

func (s *Service) wake(subjects []string) {
	for _, subject := range subjects {
		s.notifier.Notify(subject)
	}
}
