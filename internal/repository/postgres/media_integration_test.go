//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	domain "go.airlance.org/server/internal/domain/media"
	"go.airlance.org/server/internal/domain/profile"
	"go.airlance.org/server/internal/infrastructure/database"
	"go.airlance.org/server/internal/repository/postgres"
)

func TestMediaAgainstPostgres(t *testing.T) {
	url := os.Getenv("AIRLANCE_SERVER_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("AIRLANCE_SERVER_TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	if err := database.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}

	pool, err := database.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, "TRUNCATE media_objects, profiles, profile_photos"); err != nil {
		t.Fatal(err)
	}

	repository := postgres.NewMedia(pool)
	if _, err := repository.Get(ctx, "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a missing file: %v", err)
	}

	want := domain.Object{ID: "one", Owner: "anna", Size: 10, CreatedAt: time.Unix(100, 0).UTC()}
	if err := repository.Create(ctx, want); err != nil {
		t.Fatal(err)
	}

	got, err := repository.Get(ctx, "one")
	if err != nil {
		t.Fatal(err)
	}

	if got.Owner != want.Owner || got.Size != want.Size || !got.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("got %+v", got)
	}

	if err := repository.Create(ctx, want); err == nil {
		t.Fatal("the same id twice was accepted")
	}

	if public, err := repository.IsProfileMedia(ctx, "one", "boris"); err != nil || public {
		t.Fatalf("a private file: %v, %v", public, err)
	}

	next, err := profile.New("anna", profile.Fields{FirstName: "Анна"}, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}

	profiles := postgres.NewProfiles(pool)
	if err := profiles.Save(ctx, next); err != nil {
		t.Fatal(err)
	}

	if err := profiles.AddPhoto(ctx, "anna", profile.Photo{PhotoMediaID: "one", AddedAt: time.Unix(100, 0)}); err != nil {
		t.Fatal(err)
	}

	if public, err := repository.IsProfileMedia(ctx, "one", "boris"); err != nil || !public {
		t.Fatalf("an avatar: %v, %v", public, err)
	}

	if err := profiles.RemovePhoto(ctx, "anna", "one", time.Unix(101, 0)); err != nil {
		t.Fatal(err)
	}

	if public, err := repository.IsProfileMedia(ctx, "one", "boris"); err != nil || public {
		t.Fatalf("a removed photo: %v, %v", public, err)
	}
}
