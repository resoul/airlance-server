package update

import (
	"context"
	"fmt"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/profile"
	domain "go.airlance.org/server/internal/domain/update"
)

type Repository interface {
	Latest(ctx context.Context, subject string) (uint64, error)
	After(ctx context.Context, subject string, after uint64, limit int) ([]domain.Update, error)
}

type Service struct {
	repository Repository
	hub        *Hub
}

func New(repository Repository, hub *Hub) *Service {
	return &Service{repository: repository, hub: hub}
}

func (s *Service) Subscribe(ctx context.Context, principal identity.Principal, after uint64) (uint64, error) {
	if !principal.HasScope(profile.ScopeRead) {
		return 0, identity.ErrForbidden
	}

	if after > domain.MaxSeq {
		return 0, domain.ErrInvalidSeq
	}

	latest, err := s.repository.Latest(ctx, principal.Subject)
	if err != nil {
		return 0, fmt.Errorf("latest update: %w", err)
	}

	return latest, nil
}

func (s *Service) After(
	ctx context.Context,
	principal identity.Principal,
	after uint64,
	limit int,
) ([]domain.Update, error) {
	if !principal.HasScope(profile.ScopeRead) {
		return nil, identity.ErrForbidden
	}

	updates, err := s.repository.After(ctx, principal.Subject, after, limit)
	if err != nil {
		return nil, fmt.Errorf("read updates: %w", err)
	}

	return updates, nil
}

func (s *Service) Listen(subject string) (<-chan struct{}, func()) {
	return s.hub.Listen(subject)
}
