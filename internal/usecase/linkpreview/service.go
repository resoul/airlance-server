package linkpreview

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"go.airlance.org/server/internal/domain/identity"
	domain "go.airlance.org/server/internal/domain/linkpreview"
	"go.airlance.org/server/internal/domain/media"
	"go.airlance.org/server/internal/domain/message"
)

const (
	foundTTL     = 24 * time.Hour
	missingTTL   = 15 * time.Minute
	fetchTimeout = 10 * time.Second
	perMinute    = 30
	owner        = "link-preview"
	idBytes      = 16
)

type Page struct {
	Site    string
	Title   string
	Summary string
	Image   []byte
	Width   int32
	Height  int32
}

type Repository interface {
	Get(ctx context.Context, url string) (domain.Preview, time.Time, bool, error)
	Save(ctx context.Context, preview domain.Preview, now time.Time) error
}

type Fetcher interface {
	Page(ctx context.Context, url string) (Page, error)
}

type Blobs interface {
	Put(ctx context.Context, id string, data []byte) error
}

type Registry interface {
	Create(ctx context.Context, object media.Object) error
}

type Service struct {
	repository Repository
	fetcher    Fetcher
	blobs      Blobs
	registry   Registry
	now        func() time.Time
	mu         sync.Mutex
	recent     map[string][]time.Time
}

func New(repository Repository, fetcher Fetcher, blobs Blobs, registry Registry, now func() time.Time) *Service {
	return &Service{
		repository: repository, fetcher: fetcher, blobs: blobs, registry: registry, now: now,
		recent: map[string][]time.Time{},
	}
}

func (s *Service) Preview(ctx context.Context, principal identity.Principal, raw string) (domain.Preview, error) {
	if !principal.HasScope(message.ScopeRead) {
		return domain.Preview{}, identity.ErrForbidden
	}

	link, err := domain.Normalize(raw)
	if err != nil {
		return domain.Preview{}, err
	}

	cached, at, found, err := s.repository.Get(ctx, link)
	if err != nil {
		return domain.Preview{}, fmt.Errorf("read cache: %w", err)
	}

	if found && s.now().Sub(at) < ttl(cached) {
		return cached, nil
	}

	if !s.allow(principal.Subject) {
		return domain.Preview{}, domain.ErrRateLimited
	}

	return s.build(ctx, link)
}

func ttl(preview domain.Preview) time.Duration {
	if preview.Found {
		return foundTTL
	}

	return missingTTL
}

func (s *Service) allow(subject string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	kept := s.recent[subject][:0]

	for _, at := range s.recent[subject] {
		if now.Sub(at) < time.Minute {
			kept = append(kept, at)
		}
	}

	if len(kept) >= perMinute {
		s.recent[subject] = kept

		return false
	}

	s.recent[subject] = append(kept, now)

	return true
}

func (s *Service) build(ctx context.Context, link string) (domain.Preview, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fetchTimeout)
	defer cancel()

	preview := domain.Preview{URL: link}

	page, err := s.fetcher.Page(ctx, link)
	if err == nil && (page.Title != "" || page.Summary != "") {
		preview.Found = true
		preview.Site = domain.Clip(page.Site, domain.MaxSite)
		preview.Title = domain.Clip(page.Title, domain.MaxTitle)
		preview.Summary = domain.Clip(page.Summary, domain.MaxSummary)
		s.attachPhoto(ctx, &preview, page)
	}

	if err := s.repository.Save(ctx, preview, s.now()); err != nil {
		return domain.Preview{}, fmt.Errorf("save preview: %w", err)
	}

	return preview, nil
}

func (s *Service) attachPhoto(ctx context.Context, preview *domain.Preview, page Page) {
	if len(page.Image) == 0 {
		return
	}

	raw := make([]byte, idBytes)
	if _, err := rand.Read(raw); err != nil {
		return
	}

	id := hex.EncodeToString(raw)

	object, err := media.New(id, owner, int64(len(page.Image)), s.now())
	if err != nil {
		return
	}

	if err := s.blobs.Put(ctx, id, page.Image); err != nil {
		return
	}

	if err := s.registry.Create(ctx, object); err != nil {
		return
	}

	preview.PhotoMediaID, preview.Width, preview.Height = id, page.Width, page.Height
}
