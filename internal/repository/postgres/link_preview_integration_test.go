//go:build integration

package postgres_test

import (
	"testing"
	"time"

	domain "go.airlance.org/server/internal/domain/linkpreview"
	"go.airlance.org/server/internal/domain/media"
	"go.airlance.org/server/internal/repository/postgres"
)

func TestLinkPreviewsAreCachedAndTheirImageIsPublic(t *testing.T) {
	s := newStage(t)
	previews := postgres.NewLinkPreviews(s.pool)
	files := postgres.NewMedia(s.pool)
	now := time.Now().UTC().Truncate(time.Millisecond)

	if _, _, found, err := previews.Get(s.ctx, "https://example.com/a"); err != nil || found {
		t.Fatalf("an unknown link: %v, %v", found, err)
	}

	object, err := media.New("cover1", "link-preview", 100, now)
	if err != nil {
		t.Fatal(err)
	}

	if err := files.Create(s.ctx, object); err != nil {
		t.Fatal(err)
	}

	want := domain.Preview{
		Found: true, URL: "https://example.com/a", Site: "Example", Title: "Title", Summary: "About",
		PhotoMediaID: "cover1", Width: 640, Height: 320,
	}
	if err := previews.Save(s.ctx, want, now); err != nil {
		t.Fatal(err)
	}

	got, at, found, err := previews.Get(s.ctx, want.URL)
	if err != nil || !found || got != want || !at.Equal(now) {
		t.Fatalf("got %+v at %v, %v, %v", got, at, found, err)
	}

	want.Title = "New"
	if err := previews.Save(s.ctx, want, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if got, _, _, _ := previews.Get(s.ctx, want.URL); got.Title != "New" {
		t.Fatalf("a repeat save did not replace: %+v", got)
	}

	open, err := files.IsPreviewMedia(s.ctx, "cover1")
	if err != nil || !open {
		t.Fatalf("the cover is closed: %v, %v", open, err)
	}

	if open, _ := files.IsPreviewMedia(s.ctx, "other"); open {
		t.Fatal("an unrelated file is open")
	}
}
