//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"go.airlance.org/server/internal/domain/media"
	chat "go.airlance.org/server/internal/domain/message"
	domain "go.airlance.org/server/internal/domain/privacy"
	"go.airlance.org/server/internal/domain/profile"
	"go.airlance.org/server/internal/domain/story"
	"go.airlance.org/server/internal/infrastructure/database"
	"go.airlance.org/server/internal/repository/postgres"
	usecase "go.airlance.org/server/internal/usecase/contact"
)

func TestPrivacyAgainstPostgres(t *testing.T) {
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

	_, err = pool.Exec(ctx, `TRUNCATE profile_privacy, blocks, contacts, profiles, profile_photos, stories, story_views,
		media_objects, chats, chat_members, messages, updates, update_heads, search_queue CASCADE`)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Unix(1_000_000, 0).UTC()
	profiles := postgres.NewProfiles(pool)

	for _, subject := range []string{"anna", "boris", "carl"} {
		next := profile.Profile{Subject: subject, Fields: profile.Fields{FirstName: subject}, UpdatedAt: now}
		if err := profiles.Save(ctx, next); err != nil {
			t.Fatal(err)
		}
	}

	messages := postgres.NewMessages(pool)

	chatID, err := messages.OpenDirect(ctx, "chat", "anna", "boris", now)
	if err != nil {
		t.Fatal(err)
	}

	privacy := postgres.NewPrivacy(pool)
	contacts := postgres.NewContacts(pool)

	err = contacts.Add(ctx, usecase.Added{Owner: "anna", Subject: "boris", AddedAt: now})
	if err != nil {
		t.Fatal(err)
	}

	files := postgres.NewMedia(pool)

	settings, err := privacy.Settings(ctx, "anna")
	if err != nil || settings != domain.Defaults() {
		t.Fatalf("defaults: %+v, %v", settings, err)
	}

	relation, err := privacy.Relation(ctx, "boris", "anna")
	if err != nil || !relation.SharesChat || !relation.IsContact || relation.BlockedByTarget {
		t.Fatalf("a contact and chat partner: %+v, %v", relation, err)
	}

	if err := files.Create(ctx, media.Object{ID: "p", Owner: "anna", Size: 1, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}

	if err := profiles.AddPhoto(ctx, "anna", profile.Photo{PhotoMediaID: "p", AddedAt: now}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		level          domain.Level
		partner, other bool
	}{
		{domain.Everyone, true, true},
		{domain.Contacts, true, false},
		{domain.Nobody, false, false},
	} {
		next := domain.Defaults()
		next.Photos = tc.level

		if err := privacy.SaveSettings(ctx, "anna", next); err != nil {
			t.Fatal(err)
		}

		if got, err := files.IsProfileMedia(ctx, "p", "boris"); err != nil || got != tc.partner {
			t.Errorf("level %d, partner: %v, %v", tc.level, got, err)
		}

		if got, err := files.IsProfileMedia(ctx, "p", "carl"); err != nil || got != tc.other {
			t.Errorf("level %d, stranger: %v, %v", tc.level, got, err)
		}
	}

	if err := privacy.SaveSettings(ctx, "anna", domain.Defaults()); err != nil {
		t.Fatal(err)
	}

	first, err := chat.New(chatID, 1, "boris", "c1", "hi", now)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := messages.Send(ctx, first); err != nil {
		t.Fatal(err)
	}

	stories := postgres.NewStories(pool)
	next, err := story.New("s", "anna", story.Draft{ClientID: "c", MediaID: "p", LifetimeHours: 12}, now)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := stories.Publish(ctx, next); err != nil {
		t.Fatal(err)
	}

	if err := privacy.SetBlocked(ctx, "anna", "boris", true, now); err != nil {
		t.Fatal(err)
	}

	relation, err = privacy.Relation(ctx, "boris", "anna")
	if err != nil || !relation.BlockedByTarget {
		t.Fatalf("after a block: %+v, %v", relation, err)
	}

	if got, err := files.IsProfileMedia(ctx, "p", "boris"); err != nil || got {
		t.Fatalf("a blocked person's view of the photo: %v, %v", got, err)
	}

	second, err := chat.New(chatID, 2, "boris", "c2", "hi", now)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := messages.Send(ctx, second); !errors.Is(err, chat.ErrBlocked) {
		t.Fatalf("a message from a blocked person: %v", err)
	}

	third, err := chat.New(chatID, 3, "anna", "c3", "hi", now)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := messages.Send(ctx, third); !errors.Is(err, chat.ErrBlocked) {
		t.Fatalf("a message to a blocked person: %v", err)
	}

	if feed, err := stories.Feed(ctx, "boris", now, 10); err != nil || len(feed) != 0 {
		t.Fatalf("a blocked person's stories: %+v, %v", feed, err)
	}

	if visible, err := files.IsVisibleStoryMedia(ctx, "p", "boris", now); err != nil || visible {
		t.Fatalf("a blocked person's view of the story file: %v, %v", visible, err)
	}

	people, err := privacy.Blocked(ctx, "anna")
	if err != nil || len(people) != 1 || people[0].FirstName != "boris" {
		t.Fatalf("the list: %+v, %v", people, err)
	}

	blockers, err := privacy.BlockersOf(ctx, "boris")
	if err != nil || len(blockers) != 1 || blockers[0] != "anna" {
		t.Fatalf("blockers: %+v, %v", blockers, err)
	}

	if err := privacy.SetBlocked(ctx, "anna", "boris", false, now); err != nil {
		t.Fatal(err)
	}

	if feed, err := stories.Feed(ctx, "boris", now, 10); err != nil || len(feed) != 1 {
		t.Fatalf("after unblocking: %+v, %v", feed, err)
	}
}
