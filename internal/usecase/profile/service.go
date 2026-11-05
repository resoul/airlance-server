package profile

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/media"
	domain "go.airlance.org/server/internal/domain/profile"
)

type Repository interface {
	Get(ctx context.Context, subject string) (domain.Profile, error)
	Save(ctx context.Context, profile domain.Profile) error
	AddPhoto(ctx context.Context, subject string, photo domain.Photo) error
	RemovePhoto(ctx context.Context, subject, photoMediaID string, now time.Time) error
}

type Media interface {
	Get(ctx context.Context, id string) (media.Object, error)
}

type Guard interface {
	Filter(ctx context.Context, viewer string, target domain.Profile) (domain.Profile, error)
}

type Notifier interface {
	Notify(subject string)
}

type Notifiers []Notifier

func (n Notifiers) Notify(subject string) {
	for _, notifier := range n {
		notifier.Notify(subject)
	}
}

type Service struct {
	repository Repository
	files      Media
	guard      Guard
	notifier   Notifier
	now        func() time.Time
}

func New(repository Repository, files Media, guard Guard, notifier Notifier, now func() time.Time) *Service {
	return &Service{repository: repository, files: files, guard: guard, notifier: notifier, now: now}
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

func (s *Service) GetPublic(ctx context.Context, principal identity.Principal, subject string) (domain.Profile, error) {
	if !principal.HasScope(domain.ScopeRead) {
		return domain.Profile{}, identity.ErrForbidden
	}

	got, err := s.repository.Get(ctx, subject)
	if err != nil {
		return domain.Profile{}, fmt.Errorf("get profile: %w", err)
	}

	shown, err := s.guard.Filter(ctx, principal.Subject, got)
	if err != nil {
		return domain.Profile{}, fmt.Errorf("apply privacy: %w", err)
	}

	return shown, nil
}

func (s *Service) UpdateOwn(ctx context.Context, principal identity.Principal, fields domain.Fields) (domain.Profile, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return domain.Profile{}, identity.ErrForbidden
	}

	fields.PhotoMediaID = ""
	fields.VideoMediaID = ""

	next, err := domain.New(principal.Subject, fields, s.now())
	if err != nil {
		return domain.Profile{}, fmt.Errorf("check profile: %w", err)
	}

	current, err := s.repository.Get(ctx, principal.Subject)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return domain.Profile{}, fmt.Errorf("get profile: %w", err)
	}

	next.Fields.PhotoMediaID = current.Fields.PhotoMediaID
	next.Fields.VideoMediaID = current.Fields.VideoMediaID
	next.Photos = current.Photos

	if err := s.repository.Save(ctx, next); err != nil {
		return domain.Profile{}, fmt.Errorf("save profile: %w", err)
	}

	s.notifier.Notify(principal.Subject)

	return next, nil
}

func (s *Service) AddPhoto(
	ctx context.Context,
	principal identity.Principal,
	photoMediaID, videoMediaID string,
) (domain.Profile, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return domain.Profile{}, identity.ErrForbidden
	}

	photo, err := domain.NewPhoto(photoMediaID, videoMediaID, s.now())
	if err != nil {
		return domain.Profile{}, fmt.Errorf("check photo: %w", err)
	}

	if err := s.checkFile(ctx, principal.Subject, photo.PhotoMediaID, domain.MaxPhotoBytes); err != nil {
		return domain.Profile{}, err
	}

	if err := s.checkFile(ctx, principal.Subject, photo.VideoMediaID, domain.MaxVideoBytes); err != nil {
		return domain.Profile{}, err
	}

	if err := s.repository.AddPhoto(ctx, principal.Subject, photo); err != nil {
		return domain.Profile{}, fmt.Errorf("add photo: %w", err)
	}

	return s.changed(ctx, principal.Subject)
}

func (s *Service) RemovePhoto(
	ctx context.Context,
	principal identity.Principal,
	photoMediaID string,
) (domain.Profile, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return domain.Profile{}, identity.ErrForbidden
	}

	if err := s.repository.RemovePhoto(ctx, principal.Subject, photoMediaID, s.now()); err != nil {
		return domain.Profile{}, fmt.Errorf("remove photo: %w", err)
	}

	return s.changed(ctx, principal.Subject)
}

func (s *Service) changed(ctx context.Context, subject string) (domain.Profile, error) {
	s.notifier.Notify(subject)

	got, err := s.repository.Get(ctx, subject)
	if err != nil {
		return domain.Profile{}, fmt.Errorf("get profile: %w", err)
	}

	return got, nil
}

func (s *Service) checkFile(ctx context.Context, owner, id string, limit int64) error {
	if id == "" {
		return nil
	}

	object, err := s.files.Get(ctx, id)
	if errors.Is(err, media.ErrNotFound) {
		return domain.ErrInvalidMedia
	}

	if err != nil {
		return fmt.Errorf("get profile media: %w", err)
	}

	if object.Owner != owner || object.Size > limit {
		return domain.ErrInvalidMedia
	}

	return nil
}
