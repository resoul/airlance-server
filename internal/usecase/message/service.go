package message

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"go.airlance.org/server/internal/domain/identity"
	domain "go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/profile"
)

const chatIDBytes = 16

type Repository interface {
	OpenDirect(ctx context.Context, candidateID, first, second string, now time.Time) (string, error)
	Send(ctx context.Context, next domain.Message) (stored domain.Message, members []string, created bool, err error)
	History(ctx context.Context, chatID, subject string, before int64, limit int) ([]domain.Message, error)
	Chats(ctx context.Context, subject string, before int64, limit int) ([]domain.Chat, error)
	Chat(ctx context.Context, chatID, subject string) (domain.Chat, error)
	Create(ctx context.Context, next domain.NewChat, people []string) (string, []string, error)
	Join(ctx context.Context, code, subject string, now time.Time) (string, []string, error)
	IsMember(ctx context.Context, chatID, subject string) (bool, error)
	AddMember(ctx context.Context, chatID, actor, subject string, role domain.Role, now time.Time) ([]string, error)
	RemoveMember(ctx context.Context, chatID, actor, subject string) ([]string, error)
	Members(ctx context.Context, chatID, subject string, limit int) ([]domain.Member, error)
	ResetInvite(ctx context.Context, chatID, actor, token string) error
	SetPinned(ctx context.Context, chatID, subject string, isPinned bool) error
	CheckWrite(ctx context.Context, chatID, sender string) error
	RoleOf(ctx context.Context, chatID, subject string) (domain.Role, bool, error)
}

type Profiles interface {
	Get(ctx context.Context, subject string) (profile.Profile, error)
}

type Guard interface {
	CheckOpen(ctx context.Context, from, to string) error
	Filter(ctx context.Context, viewer string, target profile.Profile) (profile.Profile, error)
}

type Notifier interface {
	Notify(subject string)
}

type Service struct {
	repository Repository
	profiles   Profiles
	guard      Guard
	notifier   Notifier
	ids        *IDs
	now        func() time.Time
}

func New(
	repository Repository,
	profiles Profiles,
	guard Guard,
	notifier Notifier,
	ids *IDs,
	now func() time.Time,
) *Service {
	return &Service{repository: repository, profiles: profiles, guard: guard, notifier: notifier, ids: ids, now: now}
}

func (s *Service) OpenDirect(ctx context.Context, principal identity.Principal, peer string) (domain.Chat, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return domain.Chat{}, identity.ErrForbidden
	}

	if peer == "" || peer == principal.Subject {
		return domain.Chat{}, domain.ErrInvalidPeer
	}

	found, err := s.profiles.Get(ctx, peer)
	if errors.Is(err, profile.ErrNotFound) {
		return domain.Chat{}, domain.ErrPeerNotFound
	}

	if err != nil {
		return domain.Chat{}, fmt.Errorf("get peer: %w", err)
	}

	if err := s.guard.CheckOpen(ctx, principal.Subject, peer); err != nil {
		return domain.Chat{}, fmt.Errorf("check chat: %w", err)
	}

	found, err = s.guard.Filter(ctx, principal.Subject, found)
	if err != nil {
		return domain.Chat{}, fmt.Errorf("apply privacy: %w", err)
	}

	candidate, err := newChatID()
	if err != nil {
		return domain.Chat{}, err
	}

	id, err := s.repository.OpenDirect(ctx, candidate, principal.Subject, peer, s.now())
	if err != nil {
		return domain.Chat{}, fmt.Errorf("open chat: %w", err)
	}

	return domain.Chat{ID: id, Peer: domain.Peer{
		Subject:      peer,
		FirstName:    found.Fields.FirstName,
		LastName:     found.Fields.LastName,
		Username:     found.Fields.Username,
		PhotoMediaID: found.Fields.PhotoMediaID,
		VideoMediaID: found.Fields.VideoMediaID,
	}}, nil
}

func (s *Service) Send(
	ctx context.Context,
	principal identity.Principal,
	chatID, clientID, text string,
) (domain.Message, error) {
	return s.SendContent(ctx, principal, chatID, clientID, domain.Outgoing{Text: text})
}

func (s *Service) SendContent(
	ctx context.Context,
	principal identity.Principal,
	chatID, clientID string,
	outgoing domain.Outgoing,
) (domain.Message, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return domain.Message{}, identity.ErrForbidden
	}

	next, err := domain.Compose(chatID, s.ids.Next(), principal.Subject, clientID, outgoing, s.now())
	if err != nil {
		return domain.Message{}, fmt.Errorf("check message: %w", err)
	}

	stored, members, created, err := s.repository.Send(ctx, next)
	if err != nil {
		return domain.Message{}, fmt.Errorf("send message: %w", err)
	}

	if created {
		for _, member := range members {
			s.notifier.Notify(member)
		}
	}

	return stored, nil
}

func (s *Service) Deliver(ctx context.Context, delivery domain.Delivery) (domain.Message, error) {
	next, err := domain.New(delivery.ChatID, s.ids.Next(), delivery.Owner, delivery.ClientID, delivery.Text, s.now())
	if err != nil {
		return domain.Message{}, fmt.Errorf("check message: %w", err)
	}

	next.AgentID, next.AgentName = delivery.AgentID, delivery.AgentName

	stored, members, created, err := s.repository.Send(ctx, next)
	if err != nil {
		return domain.Message{}, fmt.Errorf("deliver message: %w", err)
	}

	if created {
		s.wake(members)
	}

	return stored, nil
}

func (s *Service) CheckWrite(ctx context.Context, chatID, sender string) error {
	if err := s.repository.CheckWrite(ctx, chatID, sender); err != nil {
		return fmt.Errorf("check rights: %w", err)
	}

	return nil
}

func (s *Service) RoleOf(ctx context.Context, chatID, subject string) (domain.Role, bool, error) {
	role, isMember, err := s.repository.RoleOf(ctx, chatID, subject)
	if err != nil {
		return 0, false, fmt.Errorf("read role: %w", err)
	}

	return role, isMember, nil
}

func (s *Service) History(
	ctx context.Context,
	principal identity.Principal,
	chatID string,
	before int64,
	limit int,
) ([]domain.Message, error) {
	if !principal.HasScope(domain.ScopeRead) {
		return nil, identity.ErrForbidden
	}

	if limit <= 0 || limit > domain.MaxHistoryPage {
		limit = domain.MaxHistoryPage
	}

	messages, err := s.repository.History(ctx, chatID, principal.Subject, before, limit)
	if err != nil {
		return nil, fmt.Errorf("read history: %w", err)
	}

	return messages, nil
}

func (s *Service) Chats(ctx context.Context, principal identity.Principal, before int64) ([]domain.Chat, error) {
	if !principal.HasScope(domain.ScopeRead) {
		return nil, identity.ErrForbidden
	}

	chats, err := s.repository.Chats(ctx, principal.Subject, before, domain.MaxChatsPage)
	if err != nil {
		return nil, fmt.Errorf("read chats: %w", err)
	}

	return chats, nil
}

func newChatID() (string, error) {
	raw := make([]byte, chatIDBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("chat id: %w", err)
	}

	return hex.EncodeToString(raw), nil
}
