//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	domain "go.airlance.org/server/internal/domain/profile"
	"go.airlance.org/server/internal/infrastructure/database"
	"go.airlance.org/server/internal/repository/postgres"
)

func TestProfilesAgainstPostgres(t *testing.T) {
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

	if _, err := pool.Exec(ctx, "TRUNCATE profiles"); err != nil {
		t.Fatal(err)
	}

	repository := postgres.NewProfiles(pool)
	if _, err := repository.Get(ctx, "anna"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing profile: %v", err)
	}

	now := time.Unix(100, 0).UTC()
	anna := domain.Profile{Subject: "anna", Fields: domain.Fields{FirstName: "Анна", Username: "Anna_s"}, UpdatedAt: now}
	if err := repository.Save(ctx, anna); err != nil {
		t.Fatal(err)
	}

	got, err := repository.Get(ctx, "anna")
	if err != nil {
		t.Fatal(err)
	}

	if got.Fields != anna.Fields || !got.UpdatedAt.Equal(now) {
		t.Fatalf("got %+v", got)
	}

	petro := domain.Profile{Subject: "petro", Fields: domain.Fields{FirstName: "П", Username: "anna_S"}, UpdatedAt: now}
	if err := repository.Save(ctx, petro); !errors.Is(err, domain.ErrUsernameTaken) {
		t.Fatalf("the same username in another case: %v", err)
	}

	petro.Fields.Username = ""
	if err := repository.Save(ctx, petro); err != nil {
		t.Fatalf("two people without a username: %v", err)
	}

	anna.Fields = domain.Fields{FirstName: "Анна", LastName: "Ш"}
	if err := repository.Save(ctx, anna); err != nil {
		t.Fatal(err)
	}

	got, err = repository.Get(ctx, "anna")
	if err != nil {
		t.Fatal(err)
	}

	if got.Fields.LastName != "Ш" || got.Fields.Username != "" {
		t.Fatalf("not replaced: %+v", got)
	}
}
