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
	"go.airlance.org/server/internal/domain/profile"
)

const (
	searchesPerWindow = 60
	searchWindow      = time.Minute
	prunedAbove       = 10_000
)

type Index interface {
	Configure(ctx context.Context) error
	Upsert(ctx context.Context, people []domain.Person) error
	Search(ctx context.Context, query string, limit int) ([]domain.Person, error)
}

type Blockers interface {
	BlockersOf(ctx context.Context, subject string) ([]string, error)
}

type window struct {
	start time.Time
	count int
}

type Service struct {
	index    Index
	blockers Blockers
	now      func() time.Time

	mu      sync.Mutex
	windows map[string]window
}

func New(index Index, blockers Blockers, now func() time.Time) *Service {
	return &Service{index: index, blockers: blockers, now: now, windows: map[string]window{}}
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
