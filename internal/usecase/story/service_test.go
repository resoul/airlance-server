package story_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/media"
	"go.airlance.org/server/internal/domain/profile"
	domain "go.airlance.org/server/internal/domain/story"
	"go.airlance.org/server/internal/usecase/story"
)

type memory struct {
	published []domain.Story
	expired   []story.Expired
	purged    int
}

func (m *memory) Publish(_ context.Context, next domain.Story) (domain.Story, []string, error) {
	m.published = append(m.published, next)

	return next, []string{"anna", "boris"}, nil
}

func (m *memory) Feed(context.Context, string, time.Time, int) ([]domain.Story, error) {
	return m.published, nil
}

func (m *memory) View(_ context.Context, id, _ string, _ time.Time) error {
	if id == "missing" {
		return domain.ErrNotFound
	}

	return nil
}

func (m *memory) Delete(_ context.Context, id, _ string, _ time.Time) ([]string, error) {
	if id == "foreign" {
		return nil, domain.ErrNotFound
	}

	return []string{"anna"}, nil
}

func (m *memory) Expired(context.Context, time.Time, int) ([]story.Expired, error) {
	return m.expired, nil
}

func (m *memory) Purge(_ context.Context, expired []story.Expired) error {
	m.purged += len(expired)
	m.expired = nil

	return nil
}

type files map[string]media.Object

func (f files) Get(_ context.Context, id string) (media.Object, error) {
	object, ok := f[id]
	if !ok {
		return media.Object{}, media.ErrNotFound
	}

	return object, nil
}

type store struct {
	removed []string
	failing bool
}

func (s *store) Remove(_ context.Context, id string) error {
	if s.failing {
		return errors.New("store is down")
	}

	s.removed = append(s.removed, id)

	return nil
}

type notifier struct {
	notified []string
}

func (n *notifier) Notify(subject string) {
	n.notified = append(n.notified, subject)
}

var (
	testFiles = files{
		"own":     {ID: "own", Owner: "anna", Size: 1 << 20},
		"large":   {ID: "large", Owner: "anna", Size: 21 << 20},
		"foreign": {ID: "foreign", Owner: "boris", Size: 1 << 20},
	}
	anna = identity.Principal{Subject: "anna", Scopes: []string{profile.ScopeRead, profile.ScopeWrite}}
)

func newService() (*story.Service, *memory, *store, *notifier) {
	repository, objects, wakes := &memory{}, &store{}, &notifier{}
	service := story.New(repository, testFiles, objects, wakes, func() time.Time {
		return time.Unix(100, 0)
	}, func(error) {})

	return service, repository, objects, wakes
}

func TestPublishChecksFilesAndWakesTheAudience(t *testing.T) {
	t.Parallel()

	service, repository, _, wakes := newService()
	ctx := context.Background()
	draft := domain.Draft{ClientID: "c", MediaID: "own", LifetimeHours: 12}

	if _, err := service.Publish(ctx, anna, draft); err != nil {
		t.Fatal(err)
	}

	if len(repository.published) != 1 || len(wakes.notified) != 2 {
		t.Fatalf("published %d, woke %v", len(repository.published), wakes.notified)
	}

	for name, change := range map[string]func(*domain.Draft){
		"foreign":     func(d *domain.Draft) { d.MediaID = "foreign" },
		"missing":     func(d *domain.Draft) { d.MediaID = "missing" },
		"large photo": func(d *domain.Draft) { d.MediaID = "large" },
		"bad cover":   func(d *domain.Draft) { d.CoverMediaID = "foreign" },
	} {
		next := draft
		change(&next)

		if _, err := service.Publish(ctx, anna, next); !errors.Is(err, domain.ErrInvalidMedia) {
			t.Errorf("%s: %v", name, err)
		}
	}

	if _, err := service.Publish(ctx, anna, domain.Draft{ClientID: "c", MediaID: "large", IsVideo: true, LifetimeHours: 12}); err != nil {
		t.Errorf("a large video: %v", err)
	}
}

func TestScopesDecideWhatAPersonMayDo(t *testing.T) {
	t.Parallel()

	service, _, _, _ := newService()
	ctx := context.Background()
	reader := identity.Principal{Subject: "anna", Scopes: []string{profile.ScopeRead}}
	nobody := identity.Principal{Subject: "anna"}
	draft := domain.Draft{ClientID: "c", MediaID: "own", LifetimeHours: 12}

	if _, err := service.Publish(ctx, reader, draft); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("publish: %v", err)
	}

	if err := service.Delete(ctx, reader, "s"); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("delete: %v", err)
	}

	if _, err := service.Feed(ctx, nobody); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("feed: %v", err)
	}

	if err := service.View(ctx, nobody, "s"); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("view: %v", err)
	}
}

func TestPurgeRemovesFilesBeforeRows(t *testing.T) {
	t.Parallel()

	service, repository, objects, _ := newService()
	repository.expired = []story.Expired{{ID: "s", MediaIDs: []string{"a", "b"}}}
	objects.failing = true

	if err := service.PurgeExpired(context.Background()); err == nil {
		t.Fatal("a failed removal was ignored")
	}

	if repository.purged != 0 {
		t.Fatal("rows went before the files")
	}

	objects.failing = false

	if err := service.PurgeExpired(context.Background()); err != nil {
		t.Fatal(err)
	}

	if repository.purged != 1 || len(objects.removed) != 2 {
		t.Fatalf("purged %d, removed %v", repository.purged, objects.removed)
	}
}
