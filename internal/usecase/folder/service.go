package folder

import (
	"context"
	"fmt"
	"time"

	domain "go.airlance.org/server/internal/domain/folder"
	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/profile"
)

type Repository interface {
	Get(ctx context.Context, owner string) (domain.List, error)
	Put(ctx context.Context, owner string, list domain.List, now time.Time) (domain.List, error)
}

type Notifier interface {
	Notify(subject string)
}

type Service struct {
	repository Repository
	notifier   Notifier
	now        func() time.Time
}

func New(repository Repository, notifier Notifier, now func() time.Time) *Service {
	return &Service{repository: repository, notifier: notifier, now: now}
}

func (s *Service) Get(ctx context.Context, principal identity.Principal) (domain.List, error) {
	if !principal.HasScope(profile.ScopeRead) {
		return domain.List{}, identity.ErrForbidden
	}

	list, err := s.repository.Get(ctx, principal.Subject)
	if err != nil {
		return domain.List{}, fmt.Errorf("read folders: %w", err)
	}

	return list, nil
}

func (s *Service) Put(ctx context.Context, principal identity.Principal, list domain.List) (domain.List, error) {
	if !principal.HasScope(profile.ScopeWrite) {
		return domain.List{}, identity.ErrForbidden
	}

	checked, err := list.Checked()
	if err != nil {
		return domain.List{}, fmt.Errorf("check folders: %w", err)
	}

	saved, err := s.repository.Put(ctx, principal.Subject, checked, s.now())
	if err != nil {
		return domain.List{}, fmt.Errorf("save folders: %w", err)
	}

	s.notifier.Notify(principal.Subject)

	return saved, nil
}
