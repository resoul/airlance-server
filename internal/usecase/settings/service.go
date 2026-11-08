package settings

import (
	"context"
	"fmt"
	"time"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/profile"
	domain "go.airlance.org/server/internal/domain/settings"
)

type Repository interface {
	Get(ctx context.Context, subject, key string) (domain.Record, error)
	Put(ctx context.Context, subject, key string, data []byte, now time.Time) (domain.Record, error)
	Wallpapers(ctx context.Context) ([]domain.Wallpaper, error)
	AddWallpaper(ctx context.Context, next domain.Wallpaper, now time.Time) error
	RemoveWallpaper(ctx context.Context, id string) (string, error)
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

func (s *Service) Get(ctx context.Context, principal identity.Principal, key string) (domain.Record, error) {
	if !principal.HasScope(profile.ScopeRead) {
		return domain.Record{}, identity.ErrForbidden
	}

	if err := domain.CheckKey(key); err != nil {
		return domain.Record{}, fmt.Errorf("check key: %w", err)
	}

	record, err := s.repository.Get(ctx, principal.Subject, key)
	if err != nil {
		return domain.Record{}, fmt.Errorf("read settings: %w", err)
	}

	return record, nil
}

func (s *Service) Put(
	ctx context.Context,
	principal identity.Principal,
	key string,
	data []byte,
) (domain.Record, error) {
	if !principal.HasScope(profile.ScopeWrite) {
		return domain.Record{}, identity.ErrForbidden
	}

	if err := domain.CheckKey(key); err != nil {
		return domain.Record{}, fmt.Errorf("check key: %w", err)
	}

	if err := domain.CheckData(data); err != nil {
		return domain.Record{}, fmt.Errorf("check data: %w", err)
	}

	record, err := s.repository.Put(ctx, principal.Subject, key, data, s.now())
	if err != nil {
		return domain.Record{}, fmt.Errorf("save settings: %w", err)
	}

	s.notifier.Notify(principal.Subject)

	return record, nil
}

func (s *Service) Wallpapers(ctx context.Context, principal identity.Principal) ([]domain.Wallpaper, error) {
	if !principal.HasScope(profile.ScopeRead) {
		return nil, identity.ErrForbidden
	}

	list, err := s.repository.Wallpapers(ctx)
	if err != nil {
		return nil, fmt.Errorf("read wallpapers: %w", err)
	}

	return list, nil
}
