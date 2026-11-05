package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	domain "go.airlance.org/server/internal/domain/agent"
	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/message"
)

const (
	idBytes    = 16
	tokenBytes = 32
	batch      = 100
	pollEvery  = time.Second
	lease      = time.Minute
)

type Repository interface {
	Save(ctx context.Context, next domain.Agent, webhookHash string, now time.Time) (domain.Agent, error)
	Count(ctx context.Context, owner string) (int, error)
	List(ctx context.Context, owner string) ([]domain.Agent, error)
	Delete(ctx context.Context, id, owner string) error
	Runs(ctx context.Context, id, owner string, limit int) ([]domain.Run, error)
	ByHook(ctx context.Context, hash string) (domain.Agent, error)
	PendingHooks(ctx context.Context, id string) (int, error)
	AddHookEvent(ctx context.Context, next domain.Agent, text string, now time.Time) error
	ClaimEvents(ctx context.Context, now, leasedUntil time.Time, limit int) ([]domain.Event, error)
	DoneEvent(ctx context.Context, id int64) error
	ForEvent(ctx context.Context, event domain.Event) ([]domain.Agent, error)
	ClaimDue(ctx context.Context, now, leasedUntil time.Time, limit int) ([]domain.Agent, error)
	SetNextRun(ctx context.Context, id string, next time.Time) error
	AddRun(ctx context.Context, id string, run domain.Run) error
	SentSince(ctx context.Context, id string, since time.Time) (int, error)
	NameOf(ctx context.Context, subject string) (string, error)
}

type Messages interface {
	RoleOf(ctx context.Context, chatID, subject string) (message.Role, bool, error)
	Deliver(ctx context.Context, next message.Delivery) (message.Message, error)
}

type Service struct {
	repository Repository
	messages   Messages
	now        func() time.Time
	onError    func(error)
}

func New(repository Repository, messages Messages, now func() time.Time, onError func(error)) *Service {
	return &Service{repository: repository, messages: messages, now: now, onError: onError}
}

type Saved struct {
	Agents       []domain.Agent
	WebhookToken string
}

func (s *Service) Save(ctx context.Context, principal identity.Principal, draft domain.Agent, rotate bool) (Saved, error) {
	if !principal.HasScope(message.ScopeWrite) {
		return Saved{}, identity.ErrForbidden
	}

	draft.Owner = principal.Subject
	draft, err := s.prepare(ctx, draft)
	if err != nil {
		return Saved{}, err
	}

	token, hash, err := webhookSecret(draft, rotate)
	if err != nil {
		return Saved{}, err
	}

	if draft.Trigger == domain.TriggerSchedule {
		draft.NextRunAt = draft.NextRun(s.now())
	}

	if _, err := s.repository.Save(ctx, draft, hash, s.now()); err != nil {
		return Saved{}, fmt.Errorf("save agent: %w", err)
	}

	agents, err := s.list(ctx, principal.Subject)
	if err != nil {
		return Saved{}, err
	}

	return Saved{Agents: agents, WebhookToken: token}, nil
}

func (s *Service) prepare(ctx context.Context, draft domain.Agent) (domain.Agent, error) {
	draft, err := draft.Checked()
	if err != nil {
		return domain.Agent{}, fmt.Errorf("check agent: %w", err)
	}

	role, isMember, err := s.messages.RoleOf(ctx, draft.ChatID, draft.Owner)
	if err != nil {
		return domain.Agent{}, fmt.Errorf("check rights: %w", err)
	}

	if !isMember {
		return domain.Agent{}, message.ErrChatNotFound
	}

	if role < message.RoleAdmin {
		return domain.Agent{}, message.ErrNotAllowed
	}

	if draft.ID != "" {
		return draft, nil
	}

	count, err := s.repository.Count(ctx, draft.Owner)
	if err != nil {
		return domain.Agent{}, fmt.Errorf("count agents: %w", err)
	}

	if count >= domain.MaxPerOwner {
		return domain.Agent{}, domain.ErrTooMany
	}

	draft.ID, err = randomHex(idBytes)
	if err != nil {
		return domain.Agent{}, err
	}

	return draft, nil
}

func webhookSecret(draft domain.Agent, rotate bool) (string, string, error) {
	if draft.Trigger != domain.TriggerWebhook || !rotate {
		return "", "", nil
	}

	token, err := randomHex(tokenBytes)
	if err != nil {
		return "", "", err
	}

	return token, HashToken(token), nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}

func randomHex(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("random: %w", err)
	}

	return hex.EncodeToString(raw), nil
}

func (s *Service) List(ctx context.Context, principal identity.Principal) ([]domain.Agent, error) {
	if !principal.HasScope(message.ScopeRead) {
		return nil, identity.ErrForbidden
	}

	return s.list(ctx, principal.Subject)
}

func (s *Service) list(ctx context.Context, owner string) ([]domain.Agent, error) {
	agents, err := s.repository.List(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("read agents: %w", err)
	}

	return agents, nil
}

func (s *Service) Delete(ctx context.Context, principal identity.Principal, id string) ([]domain.Agent, error) {
	if !principal.HasScope(message.ScopeWrite) {
		return nil, identity.ErrForbidden
	}

	if err := s.repository.Delete(ctx, id, principal.Subject); err != nil {
		return nil, fmt.Errorf("delete agent: %w", err)
	}

	return s.list(ctx, principal.Subject)
}

func (s *Service) Runs(ctx context.Context, principal identity.Principal, id string) ([]domain.Run, error) {
	if !principal.HasScope(message.ScopeRead) {
		return nil, identity.ErrForbidden
	}

	runs, err := s.repository.Runs(ctx, id, principal.Subject, domain.MaxRunsPage)
	if err != nil {
		return nil, fmt.Errorf("read runs: %w", err)
	}

	return runs, nil
}

func (s *Service) Webhook(ctx context.Context, token, text string) error {
	if token == "" {
		return domain.ErrNotFound
	}

	found, err := s.repository.ByHook(ctx, HashToken(token))
	if err != nil {
		return fmt.Errorf("find webhook: %w", err)
	}

	pending, err := s.repository.PendingHooks(ctx, found.ID)
	if err != nil {
		return fmt.Errorf("count webhooks: %w", err)
	}

	if pending >= domain.MaxPendingHooks {
		return domain.ErrTooManyHooks
	}

	if err := s.repository.AddHookEvent(ctx, found, text, s.now()); err != nil {
		return fmt.Errorf("add webhook event: %w", err)
	}

	return nil
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
		}
	}
}

func (s *Service) Drain(ctx context.Context) error {
	return errors.Join(s.drainEvents(ctx), s.drainSchedules(ctx))
}

func (s *Service) drainEvents(ctx context.Context) error {
	now := s.now()

	events, err := s.repository.ClaimEvents(ctx, now, now.Add(lease), batch)
	if err != nil {
		return fmt.Errorf("claim events: %w", err)
	}

	for _, event := range events {
		if err := s.handleEvent(ctx, event); err != nil {
			return err
		}

		if err := s.repository.DoneEvent(ctx, event.ID); err != nil {
			return fmt.Errorf("finish event: %w", err)
		}
	}

	return nil
}

func (s *Service) handleEvent(ctx context.Context, event domain.Event) error {
	agents, err := s.repository.ForEvent(ctx, event)
	if err != nil {
		return fmt.Errorf("find agents: %w", err)
	}

	name := ""
	if event.Subject != "" {
		name, err = s.repository.NameOf(ctx, event.Subject)
		if err != nil {
			return fmt.Errorf("read name: %w", err)
		}
	}

	for _, found := range agents {
		if !found.Matches(event) {
			continue
		}

		clientID := fmt.Sprintf("agent-%s-e%d", found.ID, event.ID)
		if err := s.act(ctx, found, clientID, found.Render(name, event.Text)); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) drainSchedules(ctx context.Context) error {
	now := s.now()

	due, err := s.repository.ClaimDue(ctx, now, now.Add(lease), batch)
	if err != nil {
		return fmt.Errorf("claim schedules: %w", err)
	}

	for _, found := range due {
		clientID := fmt.Sprintf("agent-%s-s%d", found.ID, found.NextRunAt.Unix())
		if err := s.act(ctx, found, clientID, found.Render("", "")); err != nil {
			return err
		}

		if err := s.repository.SetNextRun(ctx, found.ID, found.NextRun(now)); err != nil {
			return fmt.Errorf("plan next run: %w", err)
		}
	}

	return nil
}

func (s *Service) act(ctx context.Context, found domain.Agent, clientID, text string) error {
	now := s.now()

	refused, err := s.refusal(ctx, found, text, now)
	if err != nil {
		return err
	}

	if refused != nil {
		return s.record(ctx, found.ID, *refused)
	}

	_, err = s.messages.Deliver(ctx, message.Delivery{
		Owner: found.Owner, ChatID: found.ChatID, ClientID: clientID, Text: text, AgentID: found.ID, AgentName: found.Name,
	})
	if errors.Is(err, message.ErrNotAllowed) || errors.Is(err, message.ErrChatNotFound) || errors.Is(err, message.ErrInvalidText) {
		return s.record(ctx, found.ID, domain.Run{Status: domain.StatusFailed, Detail: domain.DetailNoRights, RanAt: now})
	}

	if err != nil {
		return fmt.Errorf("deliver agent message: %w", err)
	}

	return s.record(ctx, found.ID, domain.Run{Status: domain.StatusSent, Detail: found.Trigger.Detail(), RanAt: now})
}

func (s *Service) refusal(ctx context.Context, found domain.Agent, text string, now time.Time) (*domain.Run, error) {
	if text == "" {
		return &domain.Run{Status: domain.StatusSkipped, Detail: domain.DetailEmpty, RanAt: now}, nil
	}

	role, isMember, err := s.messages.RoleOf(ctx, found.ChatID, found.Owner)
	if err != nil {
		return nil, fmt.Errorf("check rights: %w", err)
	}

	if !isMember || role < message.RoleAdmin {
		return &domain.Run{Status: domain.StatusFailed, Detail: domain.DetailNoRights, RanAt: now}, nil
	}

	sent, err := s.repository.SentSince(ctx, found.ID, now.Add(-time.Hour))
	if err != nil {
		return nil, fmt.Errorf("count runs: %w", err)
	}

	if sent >= domain.MaxRunsPerHour {
		return &domain.Run{Status: domain.StatusSkipped, Detail: domain.DetailRateLimited, RanAt: now}, nil
	}

	return nil, nil //nolint:nilnil // nothing stands in the way
}

func (s *Service) record(ctx context.Context, id string, run domain.Run) error {
	if err := s.repository.AddRun(ctx, id, run); err != nil {
		return fmt.Errorf("record run: %w", err)
	}

	return nil
}
