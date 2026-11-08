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

type MessageStore interface {
	OpenDirect(ctx context.Context, candidateID, first, second string, now time.Time) (string, error)
	Send(ctx context.Context, next domain.Message) (stored domain.Message, members []string, created bool, err error)
	Edit(
		ctx context.Context,
		chatID string,
		id int64,
		subject, text string,
		entities []domain.Entity,
		now time.Time,
	) (domain.Message, []string, error)
	Delete(
		ctx context.Context,
		chatID string,
		ids []int64,
		subject string,
		forEveryone bool,
		now time.Time,
	) ([]int64, []string, error)
	History(ctx context.Context, chatID, subject string, before int64, limit int) ([]domain.Message, error)
	Thread(ctx context.Context, chatID, subject string, thread, before int64, limit int) ([]domain.Message, error)
	Pins(ctx context.Context, chatID, subject string) ([]int64, error)
	SetPin(
		ctx context.Context,
		chatID string,
		id int64,
		subject string,
		isPinned bool,
		now time.Time,
	) ([]int64, []string, error)
	PostStats(ctx context.Context, chatID, subject string, ids []int64, markViewed bool) ([]domain.PostStat, error)
	CheckWrite(ctx context.Context, chatID, sender string) error
}

type ReceiptStore interface {
	SetRead(ctx context.Context, chatID, subject string, upTo int64) (domain.ReadState, []string, error)
	SetDelivered(ctx context.Context, chatID, subject string, upTo int64) (domain.ReadState, []string, error)
	Readers(ctx context.Context, chatID, subject string, messageID int64, limit int) ([]domain.Member, error)
	TypingAudience(ctx context.Context, chatID, subject string) (domain.Peer, []string, error)
}

type InteractionStore interface {
	Vote(
		ctx context.Context,
		chatID string,
		id int64,
		subject string,
		options []int32,
		now time.Time,
	) (domain.Message, []string, error)
	ClosePoll(
		ctx context.Context,
		chatID string,
		id int64,
		subject string,
		now time.Time,
	) (domain.Message, []string, error)
	SetReaction(
		ctx context.Context,
		chatID string,
		id int64,
		subject, emoji string,
		now time.Time,
	) (domain.Message, []string, error)
}

type ChatStore interface {
	Chats(ctx context.Context, subject string, before int64, limit int) ([]domain.Chat, error)
	Chat(ctx context.Context, chatID, subject string) (domain.Chat, error)
	Create(ctx context.Context, next domain.NewChat, people []string) (string, []string, error)
	Join(ctx context.Context, code, subject string, now time.Time) (string, []string, error)
	SetArchived(ctx context.Context, chatID, subject string, isArchived bool) error
	SetDraft(ctx context.Context, chatID, subject, text string, now time.Time) (domain.DraftState, error)
	SetPinned(ctx context.Context, chatID, subject string, isPinned bool) error
	SetMuted(ctx context.Context, chatID, subject string, isMuted bool) error
	ResetInvite(ctx context.Context, chatID, actor, token string) error
}

type MemberStore interface {
	IsMember(ctx context.Context, chatID, subject string) (bool, error)
	AddMember(
		ctx context.Context,
		chatID, actor, subject string,
		role domain.Role,
		rights domain.Rights,
		now time.Time,
	) ([]string, error)
	RemoveMember(ctx context.Context, chatID, actor, subject string, ban bool, now time.Time) ([]string, error)
	SetMemberRights(ctx context.Context, chatID, actor string, rights domain.Rights) ([]string, error)
	SetInfo(ctx context.Context, chatID, actor string, info domain.ChatInfo, token string) ([]string, error)
	SetRules(ctx context.Context, chatID, actor string, rules domain.ChatRules) ([]string, error)
	Bans(ctx context.Context, chatID, actor string, limit int) ([]domain.Member, error)
	Unban(ctx context.Context, chatID, actor, subject string) error
	ChatLog(ctx context.Context, chatID, actor string, limit int) ([]domain.LogEntry, error)
	Members(ctx context.Context, chatID, subject string, limit int) ([]domain.Member, error)
	RoleOf(ctx context.Context, chatID, subject string) (domain.Role, bool, error)
}

type Repository interface {
	MessageStore
	ReceiptStore
	InteractionStore
	ChatStore
	MemberStore
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

func (s *Service) Edit(
	ctx context.Context,
	principal identity.Principal,
	chatID string,
	id int64,
	text string,
	entities []domain.Entity,
) (domain.Message, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return domain.Message{}, identity.ErrForbidden
	}

	edited, members, err := s.repository.Edit(ctx, chatID, id, principal.Subject, text, entities, s.now())
	if err != nil {
		return domain.Message{}, fmt.Errorf("edit message: %w", err)
	}

	s.wake(members)

	return edited, nil
}

func (s *Service) Delete(
	ctx context.Context,
	principal identity.Principal,
	chatID string,
	ids []int64,
	forEveryone bool,
) ([]int64, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return nil, identity.ErrForbidden
	}

	if len(ids) == 0 || len(ids) > domain.MaxDeleteBatch {
		return nil, domain.ErrInvalidDelete
	}

	removed, members, err := s.repository.Delete(ctx, chatID, ids, principal.Subject, forEveryone, s.now())
	if err != nil {
		return nil, fmt.Errorf("delete messages: %w", err)
	}

	s.wake(members)

	return removed, nil
}

func (s *Service) Thread(
	ctx context.Context,
	principal identity.Principal,
	chatID string,
	thread, before int64,
	limit int,
) ([]domain.Message, error) {
	if !principal.HasScope(domain.ScopeRead) {
		return nil, identity.ErrForbidden
	}

	if limit <= 0 || limit > domain.MaxHistoryPage {
		limit = domain.MaxHistoryPage
	}

	messages, err := s.repository.Thread(ctx, chatID, principal.Subject, thread, before, limit)
	if err != nil {
		return nil, fmt.Errorf("read thread: %w", err)
	}

	return messages, nil
}

func (s *Service) Pins(ctx context.Context, principal identity.Principal, chatID string) ([]int64, error) {
	if !principal.HasScope(domain.ScopeRead) {
		return nil, identity.ErrForbidden
	}

	pins, err := s.repository.Pins(ctx, chatID, principal.Subject)
	if err != nil {
		return nil, fmt.Errorf("read pins: %w", err)
	}

	return pins, nil
}

func (s *Service) SetPin(
	ctx context.Context,
	principal identity.Principal,
	chatID string,
	id int64,
	isPinned bool,
) ([]int64, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return nil, identity.ErrForbidden
	}

	pins, members, err := s.repository.SetPin(ctx, chatID, id, principal.Subject, isPinned, s.now())
	if err != nil {
		return nil, fmt.Errorf("pin message: %w", err)
	}

	s.wake(members)

	return pins, nil
}

func (s *Service) PostStats(
	ctx context.Context,
	principal identity.Principal,
	chatID string,
	ids []int64,
	markViewed bool,
) ([]domain.PostStat, error) {
	if !principal.HasScope(domain.ScopeRead) {
		return nil, identity.ErrForbidden
	}

	if len(ids) == 0 || len(ids) > domain.MaxStatsBatch {
		return nil, domain.ErrInvalidStats
	}

	stats, err := s.repository.PostStats(ctx, chatID, principal.Subject, ids, markViewed)
	if err != nil {
		return nil, fmt.Errorf("read post stats: %w", err)
	}

	return stats, nil
}

func (s *Service) SetRead(
	ctx context.Context,
	principal identity.Principal,
	chatID string,
	upTo int64,
) (domain.ReadState, error) {
	if !principal.HasScope(domain.ScopeRead) {
		return domain.ReadState{}, identity.ErrForbidden
	}

	state, members, err := s.repository.SetRead(ctx, chatID, principal.Subject, upTo)
	if err != nil {
		return domain.ReadState{}, fmt.Errorf("mark read: %w", err)
	}

	s.wake(members)

	return state, nil
}

func (s *Service) SetDelivered(
	ctx context.Context,
	principal identity.Principal,
	chatID string,
	upTo int64,
) (domain.ReadState, error) {
	if !principal.HasScope(domain.ScopeRead) {
		return domain.ReadState{}, identity.ErrForbidden
	}

	state, members, err := s.repository.SetDelivered(ctx, chatID, principal.Subject, upTo)
	if err != nil {
		return domain.ReadState{}, fmt.Errorf("mark delivered: %w", err)
	}

	s.wake(members)

	return state, nil
}

func (s *Service) SetReaction(
	ctx context.Context,
	principal identity.Principal,
	chatID string,
	id int64,
	emoji string,
) (domain.Message, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return domain.Message{}, identity.ErrForbidden
	}

	if err := domain.CheckReaction(emoji); err != nil {
		return domain.Message{}, fmt.Errorf("check reaction: %w", err)
	}

	reacted, members, err := s.repository.SetReaction(ctx, chatID, id, principal.Subject, emoji, s.now())
	if err != nil {
		return domain.Message{}, fmt.Errorf("set reaction: %w", err)
	}

	s.wake(members)

	return reacted, nil
}

func (s *Service) Vote(
	ctx context.Context,
	principal identity.Principal,
	chatID string,
	id int64,
	options []int32,
) (domain.Message, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return domain.Message{}, identity.ErrForbidden
	}

	voted, members, err := s.repository.Vote(ctx, chatID, id, principal.Subject, options, s.now())
	if err != nil {
		return domain.Message{}, fmt.Errorf("vote: %w", err)
	}

	s.wake(members)

	return voted, nil
}

func (s *Service) ClosePoll(
	ctx context.Context,
	principal identity.Principal,
	chatID string,
	id int64,
) (domain.Message, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return domain.Message{}, identity.ErrForbidden
	}

	closed, members, err := s.repository.ClosePoll(ctx, chatID, id, principal.Subject, s.now())
	if err != nil {
		return domain.Message{}, fmt.Errorf("close poll: %w", err)
	}

	s.wake(members)

	return closed, nil
}

func (s *Service) SetArchived(
	ctx context.Context,
	principal identity.Principal,
	chatID string,
	isArchived bool,
) (domain.Chat, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return domain.Chat{}, identity.ErrForbidden
	}

	if err := s.repository.SetArchived(ctx, chatID, principal.Subject, isArchived); err != nil {
		return domain.Chat{}, fmt.Errorf("archive chat: %w", err)
	}

	s.notifier.Notify(principal.Subject)

	return s.chatOf(ctx, chatID, principal.Subject)
}

func (s *Service) SetDraft(
	ctx context.Context,
	principal identity.Principal,
	chatID, text string,
) (domain.DraftState, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return domain.DraftState{}, identity.ErrForbidden
	}

	clean, err := domain.CheckDraft(text)
	if err != nil {
		return domain.DraftState{}, fmt.Errorf("check draft: %w", err)
	}

	state, err := s.repository.SetDraft(ctx, chatID, principal.Subject, clean, s.now())
	if err != nil {
		return domain.DraftState{}, fmt.Errorf("save draft: %w", err)
	}

	s.notifier.Notify(principal.Subject)

	return state, nil
}
