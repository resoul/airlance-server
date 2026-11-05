package media

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"go.airlance.org/server/internal/domain/identity"
	domain "go.airlance.org/server/internal/domain/media"
)

const (
	idBytes   = 16
	ticketTTL = 15 * time.Minute
)

type Repository interface {
	Create(ctx context.Context, object domain.Object) error
	Get(ctx context.Context, id string) (domain.Object, error)
	IsProfileMedia(ctx context.Context, id, viewer string) (bool, error)
	IsVisibleStoryMedia(ctx context.Context, id, viewer string, now time.Time) (bool, error)
	IsChatMedia(ctx context.Context, id, viewer string) (bool, error)
}

type Store interface {
	UploadURL(ctx context.Context, id string, size int64, ttl time.Duration) (string, error)
	DownloadURL(ctx context.Context, id string, ttl time.Duration) (string, error)
}

type Ticket struct {
	MediaID   string
	URL       string
	ExpiresAt time.Time
}

type Service struct {
	repository Repository
	store      Store
	now        func() time.Time
}

func New(repository Repository, store Store, now func() time.Time) *Service {
	return &Service{repository: repository, store: store, now: now}
}

func (s *Service) RequestUpload(ctx context.Context, principal identity.Principal, size int64) (Ticket, error) {
	id, err := newID()
	if err != nil {
		return Ticket{}, err
	}

	now := s.now()

	object, err := domain.New(id, principal.Subject, size, now)
	if err != nil {
		return Ticket{}, fmt.Errorf("check media: %w", err)
	}

	url, err := s.store.UploadURL(ctx, id, size, ticketTTL)
	if err != nil {
		return Ticket{}, fmt.Errorf("sign upload: %w", err)
	}

	if err := s.repository.Create(ctx, object); err != nil {
		return Ticket{}, fmt.Errorf("create media: %w", err)
	}

	return Ticket{MediaID: id, URL: url, ExpiresAt: now.Add(ticketTTL)}, nil
}

func (s *Service) RequestDownload(ctx context.Context, principal identity.Principal, id string) (Ticket, error) {
	object, err := s.repository.Get(ctx, id)
	if err != nil {
		return Ticket{}, fmt.Errorf("get media: %w", err)
	}

	if object.Owner != principal.Subject {
		allowed, err := s.mayRead(ctx, id, principal.Subject)
		if err != nil {
			return Ticket{}, fmt.Errorf("check media: %w", err)
		}

		if !allowed {
			return Ticket{}, fmt.Errorf("another person's media: %w", domain.ErrNotFound)
		}
	}

	url, err := s.store.DownloadURL(ctx, id, ticketTTL)
	if err != nil {
		return Ticket{}, fmt.Errorf("sign download: %w", err)
	}

	return Ticket{MediaID: id, URL: url, ExpiresAt: s.now().Add(ticketTTL)}, nil
}

func (s *Service) mayRead(ctx context.Context, id, viewer string) (bool, error) {
	public, err := s.repository.IsProfileMedia(ctx, id, viewer)
	if err != nil || public {
		return public, err
	}

	inChat, err := s.repository.IsChatMedia(ctx, id, viewer)
	if err != nil || inChat {
		return inChat, err
	}

	return s.repository.IsVisibleStoryMedia(ctx, id, viewer, s.now())
}

func newID() (string, error) {
	raw := make([]byte, idBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("media id: %w", err)
	}

	return hex.EncodeToString(raw), nil
}
