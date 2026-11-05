package story

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/media"
	"go.airlance.org/server/internal/domain/profile"
	domain "go.airlance.org/server/internal/domain/story"
)

const (
	idBytes    = 16
	purgeBatch = 200
	purgeEvery = 5 * time.Minute
)

type Expired struct {
	ID       string
	MediaIDs []string
}

type Repository interface {
	Publish(ctx context.Context, next domain.Story) (domain.Story, []string, error)
	Feed(ctx context.Context, viewer string, now time.Time, limit int) ([]domain.Story, error)
	View(ctx context.Context, id, viewer string, now time.Time) error
	Delete(ctx context.Context, id, owner string, now time.Time) ([]string, error)
	Expired(ctx context.Context, now time.Time, limit int) ([]Expired, error)
	Purge(ctx context.Context, expired []Expired) error
}

type Media interface {
	Get(ctx context.Context, id string) (media.Object, error)
}

type Files interface {
	Remove(ctx context.Context, id string) error
}

type Notifier interface {
	Notify(subject string)
}

type Service struct {
	repository Repository
	media      Media
	files      Files
	notifier   Notifier
	now        func() time.Time
	onError    func(error)
}

func New(
	repository Repository,
	media Media,
	files Files,
	notifier Notifier,
	now func() time.Time,
	onError func(error),
) *Service {
	return &Service{repository: repository, media: media, files: files, notifier: notifier, now: now, onError: onError}
}

func (s *Service) Publish(ctx context.Context, principal identity.Principal, draft domain.Draft) (domain.Story, error) {
	if !principal.HasScope(profile.ScopeWrite) {
		return domain.Story{}, identity.ErrForbidden
	}

	id, err := newID()
	if err != nil {
		return domain.Story{}, err
	}

	next, err := domain.New(id, principal.Subject, draft, s.now())
	if err != nil {
		return domain.Story{}, fmt.Errorf("check story: %w", err)
	}

	limit := domain.MaxPhotoBytes
	if draft.IsVideo {
		limit = domain.MaxVideoBytes
	}

	if err := s.checkFile(ctx, principal.Subject, draft.MediaID, limit); err != nil {
		return domain.Story{}, err
	}

	if draft.CoverMediaID != "" {
		if err := s.checkFile(ctx, principal.Subject, draft.CoverMediaID, domain.MaxCoverBytes); err != nil {
			return domain.Story{}, err
		}
	}

	stored, audience, err := s.repository.Publish(ctx, next)
	if err != nil {
		return domain.Story{}, fmt.Errorf("publish story: %w", err)
	}

	s.notifyAll(audience)

	return stored, nil
}

func (s *Service) Feed(ctx context.Context, principal identity.Principal) ([]domain.Story, error) {
	if !principal.HasScope(profile.ScopeRead) {
		return nil, identity.ErrForbidden
	}

	stories, err := s.repository.Feed(ctx, principal.Subject, s.now(), domain.MaxFeed)
	if err != nil {
		return nil, fmt.Errorf("read stories: %w", err)
	}

	return stories, nil
}

func (s *Service) View(ctx context.Context, principal identity.Principal, id string) error {
	if !principal.HasScope(profile.ScopeRead) {
		return identity.ErrForbidden
	}

	if err := s.repository.View(ctx, id, principal.Subject, s.now()); err != nil {
		return fmt.Errorf("view story: %w", err)
	}

	return nil
}

func (s *Service) Delete(ctx context.Context, principal identity.Principal, id string) error {
	if !principal.HasScope(profile.ScopeWrite) {
		return identity.ErrForbidden
	}

	audience, err := s.repository.Delete(ctx, id, principal.Subject, s.now())
	if err != nil {
		return fmt.Errorf("delete story: %w", err)
	}

	s.notifyAll(audience)

	return nil
}

func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(purgeEvery)
	defer ticker.Stop()

	for {
		if err := s.PurgeExpired(ctx); err != nil && ctx.Err() == nil {
			s.onError(err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) PurgeExpired(ctx context.Context) error {
	for {
		expired, err := s.repository.Expired(ctx, s.now(), purgeBatch)
		if err != nil {
			return fmt.Errorf("read expired stories: %w", err)
		}

		if len(expired) == 0 {
			return nil
		}

		for _, next := range expired {
			for _, id := range next.MediaIDs {
				if err := s.files.Remove(ctx, id); err != nil {
					return fmt.Errorf("remove story file: %w", err)
				}
			}
		}

		if err := s.repository.Purge(ctx, expired); err != nil {
			return fmt.Errorf("purge stories: %w", err)
		}
	}
}

func (s *Service) checkFile(ctx context.Context, owner, id string, limit int64) error {
	object, err := s.media.Get(ctx, id)
	if errors.Is(err, media.ErrNotFound) {
		return domain.ErrInvalidMedia
	}

	if err != nil {
		return fmt.Errorf("get story media: %w", err)
	}

	if object.Owner != owner || object.Size > limit {
		return domain.ErrInvalidMedia
	}

	return nil
}

func (s *Service) notifyAll(subjects []string) {
	for _, subject := range subjects {
		s.notifier.Notify(subject)
	}
}

func newID() (string, error) {
	raw := make([]byte, idBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("story id: %w", err)
	}

	return hex.EncodeToString(raw), nil
}
