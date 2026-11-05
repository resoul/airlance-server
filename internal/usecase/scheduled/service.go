package scheduled

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/message"
	domain "go.airlance.org/server/internal/domain/scheduled"
)

const (
	idBytes   = 16
	batch     = 100
	pollEvery = time.Second
	lease     = time.Minute
)

type Repository interface {
	Add(ctx context.Context, item domain.Item, now time.Time) (domain.Item, error)
	CountPending(ctx context.Context, chatID, sender string) (int, error)
	List(ctx context.Context, sender string) ([]domain.Item, error)
	Edit(ctx context.Context, id, sender, text string, sendAt time.Time) error
	Cancel(ctx context.Context, id, sender string) error
	SendNow(ctx context.Context, id, sender string, now time.Time) error
	Claim(ctx context.Context, now, leasedUntil time.Time, limit int) ([]domain.Item, error)
	Done(ctx context.Context, item domain.Item) error
	Reschedule(ctx context.Context, item domain.Item, next time.Time) error
	Fail(ctx context.Context, item domain.Item, reason string) error
}

type Messages interface {
	CheckWrite(ctx context.Context, chatID, sender string) error
	Deliver(ctx context.Context, next message.Delivery) (message.Message, error)
}

type Notifier interface {
	Notify(subject string)
}

type Service struct {
	repository Repository
	messages   Messages
	notifier   Notifier
	now        func() time.Time
	onError    func(error)
	wake       chan struct{}
}

func New(repository Repository, messages Messages, notifier Notifier, now func() time.Time, onError func(error)) *Service {
	return &Service{
		repository: repository, messages: messages, notifier: notifier, now: now, onError: onError,
		wake: make(chan struct{}, 1),
	}
}

type Draft struct {
	ClientID string
	ChatID   string
	Text     string
	SendAt   time.Time
	Repeat   domain.Repeat
	Timezone string
}

func (s *Service) Add(ctx context.Context, principal identity.Principal, draft Draft) ([]domain.Item, error) {
	if !principal.HasScope(message.ScopeWrite) {
		return nil, identity.ErrForbidden
	}

	raw := make([]byte, idBytes)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("scheduled id: %w", err)
	}

	now := s.now()

	item, err := domain.New(
		hex.EncodeToString(raw), draft.ChatID, principal.Subject, draft.ClientID, draft.Text,
		draft.SendAt, draft.Repeat, draft.Timezone, now,
	)
	if err != nil {
		return nil, fmt.Errorf("check scheduled message: %w", err)
	}

	if err := s.messages.CheckWrite(ctx, draft.ChatID, principal.Subject); err != nil {
		return nil, fmt.Errorf("check rights: %w", err)
	}

	count, err := s.repository.CountPending(ctx, draft.ChatID, principal.Subject)
	if err != nil {
		return nil, fmt.Errorf("count scheduled messages: %w", err)
	}

	if count >= domain.MaxPerChat {
		return nil, domain.ErrTooMany
	}

	if _, err := s.repository.Add(ctx, item, now); err != nil {
		return nil, fmt.Errorf("add scheduled message: %w", err)
	}

	return s.changed(ctx, principal.Subject)
}

func (s *Service) List(ctx context.Context, principal identity.Principal) ([]domain.Item, error) {
	if !principal.HasScope(message.ScopeRead) {
		return nil, identity.ErrForbidden
	}

	items, err := s.repository.List(ctx, principal.Subject)
	if err != nil {
		return nil, fmt.Errorf("read scheduled messages: %w", err)
	}

	return items, nil
}

func (s *Service) Edit(
	ctx context.Context,
	principal identity.Principal,
	id, text string,
	sendAt time.Time,
) ([]domain.Item, error) {
	if !principal.HasScope(message.ScopeWrite) {
		return nil, identity.ErrForbidden
	}

	clean, err := domain.CheckText(text)
	if err != nil {
		return nil, fmt.Errorf("check text: %w", err)
	}

	if err := domain.CheckTime(sendAt, s.now()); err != nil {
		return nil, fmt.Errorf("check time: %w", err)
	}

	if err := s.repository.Edit(ctx, id, principal.Subject, clean, sendAt); err != nil {
		return nil, fmt.Errorf("edit scheduled message: %w", err)
	}

	return s.changed(ctx, principal.Subject)
}

func (s *Service) Cancel(ctx context.Context, principal identity.Principal, id string) ([]domain.Item, error) {
	if !principal.HasScope(message.ScopeWrite) {
		return nil, identity.ErrForbidden
	}

	if err := s.repository.Cancel(ctx, id, principal.Subject); err != nil {
		return nil, fmt.Errorf("cancel scheduled message: %w", err)
	}

	return s.changed(ctx, principal.Subject)
}

func (s *Service) SendNow(ctx context.Context, principal identity.Principal, id string) ([]domain.Item, error) {
	if !principal.HasScope(message.ScopeWrite) {
		return nil, identity.ErrForbidden
	}

	if err := s.repository.SendNow(ctx, id, principal.Subject, s.now()); err != nil {
		return nil, fmt.Errorf("send scheduled message: %w", err)
	}

	s.wakeUp()

	return s.changed(ctx, principal.Subject)
}

func (s *Service) changed(ctx context.Context, subject string) ([]domain.Item, error) {
	s.notifier.Notify(subject)

	items, err := s.repository.List(ctx, subject)
	if err != nil {
		return nil, fmt.Errorf("read scheduled messages: %w", err)
	}

	return items, nil
}

func (s *Service) wakeUp() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()

	for {
		if err := s.Drain(ctx); err != nil && ctx.Err() == nil {
			s.onError(err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wake:
		}
	}
}

func (s *Service) Drain(ctx context.Context) error {
	for {
		now := s.now()

		items, err := s.repository.Claim(ctx, now, now.Add(lease), batch)
		if err != nil {
			return fmt.Errorf("claim scheduled messages: %w", err)
		}

		for _, item := range items {
			if err := s.deliver(ctx, item, now); err != nil {
				return err
			}

			s.notifier.Notify(item.Sender)
		}

		if len(items) < batch {
			return nil
		}
	}
}

func (s *Service) deliver(ctx context.Context, item domain.Item, now time.Time) error {
	_, err := s.messages.Deliver(ctx, message.Delivery{
		Owner:    item.Sender,
		ChatID:   item.ChatID,
		ClientID: item.DeliveryClientID(),
		Text:     item.Text,
	})

	reason := domain.FailureOf(err)
	if err != nil && reason == "" {
		return fmt.Errorf("deliver scheduled message: %w", err)
	}

	switch next, repeats := item.Next(now); {
	case reason != "":
		err = s.repository.Fail(ctx, item, reason)
	case repeats:
		err = s.repository.Reschedule(ctx, item, next)
	default:
		err = s.repository.Done(ctx, item)
	}

	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return fmt.Errorf("finish scheduled message: %w", err)
	}

	return nil
}
