//go:build integration

package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	domain "go.airlance.org/server/internal/domain/privacy"
	"go.airlance.org/server/internal/domain/profile"
	"go.airlance.org/server/internal/infrastructure/database"
	"go.airlance.org/server/internal/repository/postgres"
	usecase "go.airlance.org/server/internal/usecase/contact"
)

func TestContactsAgainstPostgres(t *testing.T) {
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

	if _, err := pool.Exec(ctx, `TRUNCATE contacts, blocks, profile_privacy, profiles, updates, update_heads, search_queue`); err != nil {
		t.Fatal(err)
	}

	now := time.Unix(1_000_000, 0).UTC()
	profiles := postgres.NewProfiles(pool)

	for subject, phone := range map[string]string{"anna": "", "boris": "+380 67 111 22 33", "carl": "+380 67 444 55 66"} {
		next := profile.Profile{Subject: subject, Fields: profile.Fields{FirstName: subject, Phone: phone}, UpdatedAt: now}
		if err := profiles.Save(ctx, next); err != nil {
			t.Fatal(err)
		}
	}

	contacts := postgres.NewContacts(pool)

	matched, err := contacts.Sync(ctx, "anna", []string{"380671112233", "380674445566", "1000000"}, now)
	if err != nil || len(matched) != 2 || matched["boris"] != "380671112233" {
		t.Fatalf("matched %v, %v", matched, err)
	}

	list, err := contacts.List(ctx, "anna")
	if err != nil || len(list) != 2 || !list[0].FromDevice {
		t.Fatalf("list %+v, %v", list, err)
	}

	if list[0].Phone != "" {
		t.Fatalf("a phone hidden by default was shown: %+v", list[0])
	}

	err = contacts.Add(ctx, usecase.Added{Owner: "anna", Subject: "boris", CustomFirstName: "Боря", AddedAt: now})
	if err != nil {
		t.Fatal(err)
	}

	matched, err = contacts.Sync(ctx, "anna", nil, now)
	if err != nil || len(matched) != 0 {
		t.Fatalf("an empty sync: %v, %v", matched, err)
	}

	list, err = contacts.List(ctx, "anna")
	if err != nil || len(list) != 1 || list[0].Subject != "boris" || list[0].CustomFirstName != "Боря" {
		t.Fatalf("a manual contact must stay and a device one must go: %+v, %v", list, err)
	}

	hidden := domain.Defaults()
	hidden.Search = domain.Nobody

	if err := postgres.NewPrivacy(pool).SaveSettings(ctx, "carl", hidden); err != nil {
		t.Fatal(err)
	}

	if found, err := contacts.FindByPhone(ctx, "anna", "380674445566"); err != nil || len(found) != 0 {
		t.Fatalf("a hidden person was found: %v, %v", found, err)
	}

	if err := postgres.NewPrivacy(pool).SetBlocked(ctx, "boris", "anna", true, now); err != nil {
		t.Fatal(err)
	}

	if found, err := contacts.FindByPhone(ctx, "anna", "380671112233"); err != nil || len(found) != 0 {
		t.Fatalf("found by someone blocked: %v, %v", found, err)
	}

	if list, err := contacts.List(ctx, "anna"); err != nil || len(list) != 0 {
		t.Fatalf("listed by someone blocked: %+v, %v", list, err)
	}
}
