package directory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	domain "go.airlance.org/server/internal/domain/directory"
	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/profile"
)

const (
	searchesPerWindow = 60
	searchWindow      = time.Minute
	prunedAbove       = 10_000
	clientPrefix      = "client:"
)

type Index interface {
	Configure(ctx context.Context) error
	Upsert(ctx context.Context, people []domain.Person) error
	Search(ctx context.Context, query string, limit int) ([]domain.Person, error)
}

type Blockers interface {
	BlockersOf(ctx context.Context, subject string) ([]string, error)
}

type PublicChats interface {
	SearchPublic(ctx context.Context, subject, query string, limit int) ([]message.Chat, error)
	ChatByCode(ctx context.Context, code, subject string) (message.Chat, error)
	PersonByUsername(ctx context.Context, username, viewer string) (domain.Person, error)
}

type window struct {
	start time.Time
	count int
}

type Service struct {
	index    Index
	blockers Blockers
	chats    PublicChats
	now      func() time.Time

	mu      sync.Mutex
	windows map[string]window
}

func New(index Index, blockers Blockers, chats PublicChats, now func() time.Time) *Service {
	return &Service{index: index, blockers: blockers, chats: chats, now: now, windows: map[string]window{}}
}

func (s *Service) SearchChats(ctx context.Context, principal identity.Principal, raw string) ([]message.Chat, error) {
	if !principal.HasScope(message.ScopeRead) {
		return nil, identity.ErrForbidden
	}

	query, err := domain.NormalizeQuery(raw)
	if err != nil {
		return nil, fmt.Errorf("check query: %w", err)
	}

	if !s.allow(principal.Subject) {
		return nil, domain.ErrTooManySearches
	}

	chats, err := s.chats.SearchPublic(ctx, principal.Subject, query, domain.MaxResults)
	if err != nil {
		return nil, fmt.Errorf("search chats: %w", err)
	}

	return chats, nil
}

func (s *Service) Search(ctx context.Context, principal identity.Principal, raw string) ([]domain.Person, error) {
	if !principal.HasScope(profile.ScopeRead) {
		return nil, identity.ErrForbidden
	}

	query, err := domain.NormalizeQuery(raw)
	if err != nil {
		return nil, fmt.Errorf("check query: %w", err)
	}

	if !s.allow(principal.Subject) {
		return nil, domain.ErrTooManySearches
	}

	found, err := s.index.Search(ctx, query, domain.MaxResults+1)
	if err != nil {
		return nil, errors.Join(domain.ErrSearchUnavailable, err)
	}

	blockers, err := s.blockers.BlockersOf(ctx, principal.Subject)
	if err != nil {
		return nil, fmt.Errorf("read blockers: %w", err)
	}

	people := make([]domain.Person, 0, len(found))

	for _, person := range found {
		isBlocked := slices.Contains(blockers, person.Subject)
		if !isBlocked && person.Subject != principal.Subject && len(people) < domain.MaxResults {
			people = append(people, person)
		}
	}

	return people, nil
}

func (s *Service) ResolveChat(ctx context.Context, principal identity.Principal, code string) (message.Chat, error) {
	if !principal.HasScope(message.ScopeRead) {
		return message.Chat{}, identity.ErrForbidden
	}

	return s.chatByCode(ctx, principal.Subject, code, principal.Subject)
}

func (s *Service) ResolveUser(ctx context.Context, principal identity.Principal, username string) (domain.Person, error) {
	if !principal.HasScope(profile.ScopeRead) {
		return domain.Person{}, identity.ErrForbidden
	}

	return s.personByUsername(ctx, principal.Subject, username, principal.Subject)
}

func (s *Service) PreviewChat(ctx context.Context, client, code string) (message.Chat, error) {
	return s.chatByCode(ctx, clientPrefix+client, code, "")
}

func (s *Service) PreviewUser(ctx context.Context, client, username string) (domain.Person, error) {
	return s.personByUsername(ctx, clientPrefix+client, username, "")
}

func (s *Service) chatByCode(ctx context.Context, limitedBy, code, subject string) (message.Chat, error) {
	if !s.allow(limitedBy) {
		return message.Chat{}, domain.ErrTooManySearches
	}

	chat, err := s.chats.ChatByCode(ctx, code, subject)
	if err != nil {
		return message.Chat{}, fmt.Errorf("resolve chat: %w", err)
	}

	return chat, nil
}

func (s *Service) personByUsername(ctx context.Context, limitedBy, username, viewer string) (domain.Person, error) {
	if !s.allow(limitedBy) {
		return domain.Person{}, domain.ErrTooManySearches
	}

	person, err := s.chats.PersonByUsername(ctx, username, viewer)
	if err != nil {
		return domain.Person{}, fmt.Errorf("resolve person: %w", err)
	}

	return person, nil
}

func (s *Service) allow(subject string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if len(s.windows) > prunedAbove {
		for key, current := range s.windows {
			if now.Sub(current.start) >= searchWindow {
				delete(s.windows, key)
			}
		}
	}

	current := s.windows[subject]
	if now.Sub(current.start) >= searchWindow {
		current = window{start: now}
	}

	if current.count >= searchesPerWindow {
		return false
	}

	current.count++
	s.windows[subject] = current

	return true
}
