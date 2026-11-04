package profile

import (
	"context"
	"fmt"
	"time"

	"go.airlance.org/server/internal/domain/identity"
	domain "go.airlance.org/server/internal/domain/profile"
)

type Repository interface {
	Get(ctx context.Context, subject string) (domain.Profile, error)
	Save(ctx context.Context, profile domain.Profile) error
}

type Service struct {
	repository Repository
	now        func() time.Time
}

func New(repository Repository, now func() time.Time) *Service {
	return &Service{repository: repository, now: now}
}

func (s *Service) GetOwn(ctx context.Context, principal identity.Principal) (domain.Profile, error) {
	if !principal.HasScope(domain.ScopeRead) {
		return domain.Profile{}, identity.ErrForbidden
	}

	got, err := s.repository.Get(ctx, principal.Subject)
	if err != nil {
		return domain.Profile{}, fmt.Errorf("get profile: %w", err)
	}

	return got, nil
}

func (s *Service) UpdateOwn(ctx context.Context, principal identity.Principal, fields domain.Fields) (domain.Profile, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return domain.Profile{}, identity.ErrForbidden
	}

	next, err := domain.New(principal.Subject, fields, s.now())
	if err != nil {
		return domain.Profile{}, fmt.Errorf("check profile: %w", err)
	}

	if err := s.repository.Save(ctx, next); err != nil {
		return domain.Profile{}, fmt.Errorf("save profile: %w", err)
	}

	return next, nil
}
