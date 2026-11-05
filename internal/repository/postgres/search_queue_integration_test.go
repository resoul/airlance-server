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

func TestSearchQueueAgainstPostgres(t *testing.T) {
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

	if _, err := pool.Exec(ctx, "TRUNCATE profiles, updates, update_heads, search_queue"); err != nil {
		t.Fatal(err)
	}

	profiles := postgres.NewProfiles(pool)
	queue := postgres.NewSearchQueue(pool)
	save := func(subject, name, username string) error {
		return profiles.Save(ctx, domain.Profile{
			Subject:   subject,
			Fields:    domain.Fields{FirstName: name, Username: username},
			UpdatedAt: time.Unix(100, 0).UTC(),
		})
	}

	if err := save("anna", "Анна", "anna"); err != nil {
		t.Fatal(err)
	}

	if err := save("petro", "Петро", ""); err != nil {
		t.Fatal(err)
	}

	if err := save("olena", "Олена", "ANNA"); !errors.Is(err, domain.ErrUsernameTaken) {
		t.Fatalf("a refused save: %v", err)
	}

	pending, err := queue.Pending(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(pending) != 2 || pending[0].Person.Subject != "anna" || pending[0].Person.Username != "anna" {
		t.Fatalf("a refused save must not be queued: %+v", pending)
	}

	if pending[1].Person.Username != "" || pending[1].Person.FirstName != "Петро" {
		t.Fatalf("got %+v", pending[1])
	}

	if err := save("anna", "Аня", "anna"); err != nil {
		t.Fatal(err)
	}

	if err := queue.Done(ctx, pending); err != nil {
		t.Fatal(err)
	}

	left, err := queue.Pending(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(left) != 1 || left[0].Person.Subject != "anna" || left[0].Person.FirstName != "Аня" {
		t.Fatalf("a change made during indexing must stay queued: %+v", left)
	}

	if err := queue.Done(ctx, left); err != nil {
		t.Fatal(err)
	}

	if empty, err := queue.Pending(ctx, 10); err != nil || len(empty) != 0 {
		t.Fatalf("got %+v, %v", empty, err)
	}
}
