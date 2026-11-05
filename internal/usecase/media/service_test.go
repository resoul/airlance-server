package media_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.airlance.org/server/internal/domain/identity"
	domain "go.airlance.org/server/internal/domain/media"
	"go.airlance.org/server/internal/usecase/media"
)

type memoryRepository struct {
	saved map[string]domain.Object
}

func (m *memoryRepository) Create(_ context.Context, object domain.Object) error {
	m.saved[object.ID] = object

	return nil
}

func (m *memoryRepository) Get(_ context.Context, id string) (domain.Object, error) {
	object, ok := m.saved[id]
	if !ok {
		return domain.Object{}, domain.ErrNotFound
	}

	return object, nil
}

type fakeStore struct {
	failing bool
}

func (s fakeStore) UploadURL(_ context.Context, id string, _ int64, _ time.Duration) (string, error) {
	if s.failing {
		return "", errors.New("store is down")
	}

	return "upload/" + id, nil
}

func (s fakeStore) DownloadURL(_ context.Context, id string, _ time.Duration) (string, error) {
	if s.failing {
		return "", errors.New("store is down")
	}

	return "download/" + id, nil
}

func newService(store fakeStore) (*media.Service, *memoryRepository) {
	repository := &memoryRepository{saved: map[string]domain.Object{}}
	now := time.Unix(100, 0)

	return media.New(repository, store, func() time.Time {
		return now
	}), repository
}

var anna = identity.Principal{Subject: "anna"}

func TestAnUploadGivesAPersonTheirOwnNewFile(t *testing.T) {
	t.Parallel()

	service, repository := newService(fakeStore{})
	ctx := context.Background()

	first, err := service.RequestUpload(ctx, anna, 10)
	if err != nil {
		t.Fatal(err)
	}

	second, err := service.RequestUpload(ctx, anna, 10)
	if err != nil {
		t.Fatal(err)
	}

	if first.MediaID == second.MediaID || len(first.MediaID) != 32 {
		t.Fatalf("ids %q and %q", first.MediaID, second.MediaID)
	}

	if first.URL != "upload/"+first.MediaID || first.ExpiresAt.Unix() != 100+15*60 {
		t.Fatalf("got %+v", first)
	}

	saved := repository.saved[first.MediaID]
	if saved.Owner != "anna" || saved.Size != 10 || saved.CreatedAt.Unix() != 100 {
		t.Fatalf("saved %+v", saved)
	}
}

func TestASizeOutsideTheLimitIsRefused(t *testing.T) {
	t.Parallel()

	service, repository := newService(fakeStore{})

	for _, size := range []int64{-1, 0, domain.MaxSize + 1} {
		_, err := service.RequestUpload(context.Background(), anna, size)
		if !errors.Is(err, domain.ErrInvalidSize) {
			t.Errorf("size %d: %v", size, err)
		}
	}

	if _, err := service.RequestUpload(context.Background(), anna, domain.MaxSize); err != nil {
		t.Errorf("the largest size: %v", err)
	}

	if len(repository.saved) != 1 {
		t.Fatalf("saved %d", len(repository.saved))
	}
}

func TestAnotherPersonCannotTellAFileExists(t *testing.T) {
	t.Parallel()

	service, _ := newService(fakeStore{})
	ctx := context.Background()

	upload, err := service.RequestUpload(ctx, anna, 10)
	if err != nil {
		t.Fatal(err)
	}

	own, err := service.RequestDownload(ctx, anna, upload.MediaID)
	if err != nil || own.URL != "download/"+upload.MediaID {
		t.Fatalf("got %+v, %v", own, err)
	}

	petro := identity.Principal{Subject: "petro"}
	if _, err := service.RequestDownload(ctx, petro, upload.MediaID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("another person's file: %v", err)
	}

	if _, err := service.RequestDownload(ctx, anna, "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a missing file: %v", err)
	}
}

func TestNothingIsSavedWhenTheStoreCannotSign(t *testing.T) {
	t.Parallel()

	service, repository := newService(fakeStore{failing: true})

	if _, err := service.RequestUpload(context.Background(), anna, 10); err == nil {
		t.Fatal("expected an error")
	}

	if len(repository.saved) != 0 {
		t.Fatal("saved anyway")
	}
}
