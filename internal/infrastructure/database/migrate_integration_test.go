//go:build integration

package database_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"go.airlance.org/server/internal/infrastructure/database"
)

func scratchDatabase(ctx context.Context, t *testing.T) string {
	t.Helper()

	raw := os.Getenv("AIRLANCE_SERVER_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("AIRLANCE_SERVER_TEST_DATABASE_URL is not set")
	}

	admin, err := database.Open(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)

	name := fmt.Sprintf("migrate_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})

	address, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}

	address.Path = "/" + name

	return address.String()
}

func tableExists(ctx context.Context, t *testing.T, address, table string) bool {
	t.Helper()

	pool, err := database.Open(ctx, address)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var found bool

	err = pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&found)
	if err != nil {
		t.Fatal(err)
	}

	return found
}

func TestMigrationsGoUpAndDownCleanly(t *testing.T) {
	ctx := context.Background()
	address := scratchDatabase(ctx, t)

	state, err := database.MigrationVersion(ctx, address)
	if err != nil || state.Applied {
		t.Fatalf("a new database: %+v, %v", state, err)
	}

	if err := database.Migrate(ctx, address); err != nil {
		t.Fatal(err)
	}

	if err := database.Migrate(ctx, address); err != nil {
		t.Fatalf("a repeat must change nothing: %v", err)
	}

	state, err = database.MigrationVersion(ctx, address)
	if err != nil || !state.Applied || state.IsDirty || state.Version != 5 {
		t.Fatalf("got %+v, %v", state, err)
	}

	if !tableExists(ctx, t, address, "messages_15") {
		t.Fatal("the partitions are missing")
	}

	if err := database.MigrateDown(ctx, address, 5); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"profiles", "messages", "messages_15", "profile_photos_position", "agents", "scheduled_messages", "message_media", "chat_folder_lists", "poll_votes", "user_settings", "wallpapers", "chat_log", "link_previews"} {
		if tableExists(ctx, t, address, table) {
			t.Fatalf("%s survived the rollback", table)
		}
	}

	if err := database.Migrate(ctx, address); err != nil {
		t.Fatalf("up after down: %v", err)
	}

	if err := database.MigrateReset(ctx, address); err != nil {
		t.Fatal(err)
	}

	if err := database.MigrateReset(ctx, address); err != nil {
		t.Fatalf("a repeated reset: %v", err)
	}

	if err := database.MigrateDown(ctx, address, 0); err == nil {
		t.Fatal("zero steps must be refused")
	}
}
