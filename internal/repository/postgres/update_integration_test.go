//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	domain "go.airlance.org/server/internal/domain/profile"
	"go.airlance.org/server/internal/domain/update"
	"go.airlance.org/server/internal/infrastructure/database"
	"go.airlance.org/server/internal/repository/postgres"
)

func TestUpdatesAgainstPostgres(t *testing.T) {
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

	if _, err := pool.Exec(ctx, "TRUNCATE profiles, updates, update_heads"); err != nil {
		t.Fatal(err)
	}

	profiles := postgres.NewProfiles(pool)
	updates := postgres.NewUpdates(pool)
	now := time.Unix(100, 0).UTC()

	save := func(subject, name, username string) error {
		return profiles.Save(ctx, domain.Profile{
			Subject:   subject,
			Fields:    domain.Fields{FirstName: name, Username: username},
			UpdatedAt: now,
		})
	}

	if latest, err := updates.Latest(ctx, "anna"); err != nil || latest != 0 {
		t.Fatalf("nothing yet: %d, %v", latest, err)
	}

	if err := save("anna", "Анна", "anna"); err != nil {
		t.Fatal(err)
	}

	if err := save("petro", "Петро", ""); err != nil {
		t.Fatal(err)
	}

	if err := save("anna", "Аня", "anna"); err != nil {
		t.Fatal(err)
	}

	if err := save("petro", "Петро", "ANNA"); !errors.Is(err, domain.ErrUsernameTaken) {
		t.Fatalf("a refused save: %v", err)
	}

	if latest, err := updates.Latest(ctx, "petro"); err != nil || latest != 1 {
		t.Fatalf("a refused save must leave no update: %d, %v", latest, err)
	}

	got, err := updates.After(ctx, "anna", 0, 10)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0].Seq != 1 || got[1].Seq != 2 || got[1].Kind != update.KindProfile {
		t.Fatalf("got %+v", got)
	}

	if got[1].Profile.Fields.FirstName != "Аня" || got[1].Profile.Fields.Username != "anna" {
		t.Fatalf("got %+v", got[1])
	}

	if page, err := updates.After(ctx, "anna", 1, 10); err != nil || len(page) != 1 || page[0].Seq != 2 {
		t.Fatalf("after a number: %+v, %v", page, err)
	}

	if page, err := updates.After(ctx, "anna", 0, 1); err != nil || len(page) != 1 || page[0].Seq != 1 {
		t.Fatalf("a limit: %+v, %v", page, err)
	}
}

func TestParallelSavesLeaveNoGaps(t *testing.T) {
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

	if _, err := pool.Exec(ctx, "TRUNCATE profiles, updates, update_heads"); err != nil {
		t.Fatal(err)
	}

	const saves = 20

	profiles := postgres.NewProfiles(pool)

	var group sync.WaitGroup
	for i := range saves {
		group.Go(func() {
			err := profiles.Save(ctx, domain.Profile{
				Subject:   "anna",
				Fields:    domain.Fields{FirstName: fmt.Sprintf("Анна %d", i)},
				UpdatedAt: time.Unix(100, 0).UTC(),
			})
			if err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()

	got, err := postgres.NewUpdates(pool).After(ctx, "anna", 0, saves+1)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != saves {
		t.Fatalf("got %d updates", len(got))
	}

	for i, next := range got {
		if next.Seq != uint64(i+1) {
			t.Fatalf("update %d has number %d", i, next.Seq)
		}
	}
}
