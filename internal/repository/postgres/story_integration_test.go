//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"go.airlance.org/server/internal/domain/media"
	"go.airlance.org/server/internal/domain/profile"
	domain "go.airlance.org/server/internal/domain/story"
	"go.airlance.org/server/internal/infrastructure/database"
	"go.airlance.org/server/internal/repository/postgres"
)

func TestStoriesAgainstPostgres(t *testing.T) {
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

	_, err = pool.Exec(ctx, `TRUNCATE stories, story_views, media_objects, profiles, chats, chat_members, updates, update_heads CASCADE`)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Unix(1_000_000, 0).UTC()
	profiles := postgres.NewProfiles(pool)

	for _, subject := range []string{"anna", "boris", "carl"} {
		if err := profiles.Save(ctx, profile.Profile{Subject: subject, Fields: profile.Fields{FirstName: subject}, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := postgres.NewMessages(pool).OpenDirect(ctx, "chat", "anna", "boris", now); err != nil {
		t.Fatal(err)
	}

	stories := postgres.NewStories(pool)
	files := postgres.NewMedia(pool)

	if err := files.Create(ctx, media.Object{ID: "m", Owner: "anna", Size: 1, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}

	next, err := domain.New("s1", "anna", domain.Draft{ClientID: "c1", MediaID: "m", LifetimeHours: 12, Meta: "{}"}, now)
	if err != nil {
		t.Fatal(err)
	}

	stored, audience, err := stories.Publish(ctx, next)
	if err != nil {
		t.Fatal(err)
	}

	if stored.ID != "s1" || stored.Owner.FirstName != "anna" || len(audience) != 2 {
		t.Fatalf("published %+v to %v", stored, audience)
	}

	retry := next
	retry.ID = "s2"

	again, audience, err := stories.Publish(ctx, retry)
	if err != nil || again.ID != "s1" || len(audience) != 0 {
		t.Fatalf("a retry: %+v, %v, %v", again, audience, err)
	}

	for viewer, want := range map[string]int{"anna": 1, "boris": 1, "carl": 0} {
		feed, err := stories.Feed(ctx, viewer, now, 10)
		if err != nil || len(feed) != want {
			t.Fatalf("%s sees %d stories, %v", viewer, len(feed), err)
		}
	}

	if visible, err := files.IsVisibleStoryMedia(ctx, "m", "boris", now); err != nil || !visible {
		t.Fatalf("a partner's view of the file: %v, %v", visible, err)
	}

	if visible, err := files.IsVisibleStoryMedia(ctx, "m", "carl", now); err != nil || visible {
		t.Fatalf("a stranger's view of the file: %v, %v", visible, err)
	}

	if err := stories.View(ctx, "s1", "carl", now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a stranger viewed: %v", err)
	}

	for range 2 {
		if err := stories.View(ctx, "s1", "boris", now); err != nil {
			t.Fatal(err)
		}
	}

	feed, err := stories.Feed(ctx, "boris", now, 10)
	if err != nil || !feed[0].Seen {
		t.Fatalf("not seen: %+v, %v", feed, err)
	}

	own, err := stories.Feed(ctx, "anna", now, 10)
	if err != nil || own[0].ViewCount != 1 || own[0].Seen {
		t.Fatalf("own story: %+v, %v", own, err)
	}

	if _, err := stories.Delete(ctx, "s1", "boris", now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a foreign delete: %v", err)
	}

	later := now.Add(13 * time.Hour)

	if feed, err := stories.Feed(ctx, "boris", later, 10); err != nil || len(feed) != 0 {
		t.Fatalf("an expired story: %+v, %v", feed, err)
	}

	if visible, err := files.IsVisibleStoryMedia(ctx, "m", "boris", later); err != nil || visible {
		t.Fatalf("an expired file: %v, %v", visible, err)
	}

	expired, err := stories.Expired(ctx, later, 10)
	if err != nil || len(expired) != 1 || expired[0].MediaIDs[0] != "m" {
		t.Fatalf("expired: %+v, %v", expired, err)
	}

	if err := stories.Purge(ctx, expired); err != nil {
		t.Fatal(err)
	}

	if _, err := files.Get(ctx, "m"); !errors.Is(err, media.ErrNotFound) {
		t.Fatalf("the file row stayed: %v", err)
	}

	updates, err := postgres.NewUpdates(pool).After(ctx, "boris", 0, 10)
	if err != nil || len(updates) == 0 {
		t.Fatalf("the journal: %+v, %v", updates, err)
	}
}
