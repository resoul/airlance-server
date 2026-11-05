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

	"github.com/jackc/pgx/v5/pgxpool"

	domain "go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/profile"
	"go.airlance.org/server/internal/domain/update"
	"go.airlance.org/server/internal/infrastructure/database"
	"go.airlance.org/server/internal/repository/postgres"
)

func openMessages(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()

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
	t.Cleanup(pool.Close)

	_, err = pool.Exec(ctx, "TRUNCATE profiles, updates, update_heads, search_queue, messages, chat_members, chats")
	if err != nil {
		t.Fatal(err)
	}

	return ctx, pool
}

func message(chatID string, id int64, sender, clientID, text string) domain.Message {
	return domain.Message{
		ChatID:   chatID,
		ID:       id,
		Sender:   sender,
		ClientID: clientID,
		Text:     text,
		SentAt:   time.UnixMilli(id).UTC(),
	}
}

func TestADirectChatIsOpenedOnce(t *testing.T) {
	ctx, pool := openMessages(t)
	repository := postgres.NewMessages(pool)
	now := time.Unix(100, 0).UTC()

	first, err := repository.OpenDirect(ctx, "one", "anna", "petro", now)
	if err != nil || first != "one" {
		t.Fatalf("got %q, %v", first, err)
	}

	second, err := repository.OpenDirect(ctx, "two", "petro", "anna", now)
	if err != nil || second != "one" {
		t.Fatalf("the other way round: %q, %v", second, err)
	}

	other, err := repository.OpenDirect(ctx, "three", "anna", "olena", now)
	if err != nil || other != "three" {
		t.Fatalf("another person: %q, %v", other, err)
	}
}

func TestAMessageReachesBothJournalsOnce(t *testing.T) {
	ctx, pool := openMessages(t)
	repository := postgres.NewMessages(pool)
	updates := postgres.NewUpdates(pool)

	chat, err := repository.OpenDirect(ctx, "one", "anna", "petro", time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}

	stored, members, created, err := repository.Send(ctx, message(chat, 10, "anna", "local-1", "привет"))
	if err != nil || !created || len(members) != 2 || stored.ID != 10 {
		t.Fatalf("got %+v, %v, %v, %v", stored, members, created, err)
	}

	repeat, _, created, err := repository.Send(ctx, message(chat, 11, "anna", "local-1", "другое"))
	if err != nil || created || repeat.ID != 10 || repeat.Text != "привет" {
		t.Fatalf("a repeat: %+v, %v, %v", repeat, created, err)
	}

	for _, subject := range []string{"anna", "petro"} {
		got, err := updates.After(ctx, subject, 0, 10)
		if err != nil {
			t.Fatal(err)
		}

		if len(got) != 1 || got[0].Seq != 1 || got[0].Kind != update.KindMessage || got[0].Message.Text != "привет" {
			t.Fatalf("%s: %+v", subject, got)
		}

		if got[0].Message.Sender != "anna" || got[0].Message.ClientID != "local-1" {
			t.Fatalf("%s: %+v", subject, got[0].Message)
		}
	}
}

func TestAStrangerSeesNothingOfAChat(t *testing.T) {
	ctx, pool := openMessages(t)
	repository := postgres.NewMessages(pool)

	chat, err := repository.OpenDirect(ctx, "one", "anna", "petro", time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := repository.Send(ctx, message(chat, 10, "olena", "x", "hi")); !errors.Is(err, domain.ErrChatNotFound) {
		t.Fatalf("a stranger sending: %v", err)
	}

	if _, _, _, err := repository.Send(ctx, message("missing", 10, "anna", "x", "hi")); !errors.Is(err, domain.ErrChatNotFound) {
		t.Fatalf("a missing chat: %v", err)
	}

	if _, err := repository.History(ctx, chat, "olena", 0, 10); !errors.Is(err, domain.ErrChatNotFound) {
		t.Fatalf("a stranger reading: %v", err)
	}

	if chats, err := repository.Chats(ctx, "olena", 0, 10); err != nil || len(chats) != 0 {
		t.Fatalf("a stranger's list: %+v, %v", chats, err)
	}
}

func TestHistoryAndChatsPageFromTheNewest(t *testing.T) {
	ctx, pool := openMessages(t)
	repository := postgres.NewMessages(pool)
	now := time.Unix(100, 0).UTC()

	err := postgres.NewProfiles(pool).Save(ctx, profile.Profile{
		Subject:   "petro",
		Fields:    profile.Fields{FirstName: "Петро", LastName: "Ш", Username: "petro"},
		UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	chat, err := repository.OpenDirect(ctx, "one", "anna", "petro", now)
	if err != nil {
		t.Fatal(err)
	}

	empty, err := repository.OpenDirect(ctx, "empty", "anna", "olena", now)
	if err != nil || empty != "empty" {
		t.Fatal(err)
	}

	for id := int64(1); id <= 5; id++ {
		if _, _, _, err := repository.Send(ctx, message(chat, id, "anna", fmt.Sprint(id), fmt.Sprint("m", id))); err != nil {
			t.Fatal(err)
		}
	}

	page, err := repository.History(ctx, chat, "petro", 0, 2)
	if err != nil || len(page) != 2 || page[0].ID != 5 || page[1].ID != 4 {
		t.Fatalf("the newest: %+v, %v", page, err)
	}

	page, err = repository.History(ctx, chat, "petro", 4, 10)
	if err != nil || len(page) != 3 || page[0].ID != 3 || page[2].ID != 1 {
		t.Fatalf("older: %+v, %v", page, err)
	}

	chats, err := repository.Chats(ctx, "anna", 0, 10)
	if err != nil || len(chats) != 1 {
		t.Fatalf("a chat with no messages stays out: %+v, %v", chats, err)
	}

	got := chats[0]
	if got.ID != chat || got.Peer.Username != "petro" || got.Peer.FirstName != "Петро" {
		t.Fatalf("got %+v", got)
	}

	if got.Last == nil || got.Last.ID != 5 || got.Last.Text != "m5" {
		t.Fatalf("the last message: %+v", got.Last)
	}

	if older, err := repository.Chats(ctx, "anna", 5, 10); err != nil || len(older) != 0 {
		t.Fatalf("the next page: %+v, %v", older, err)
	}
}

func TestProfileAndMessageUpdatesShareOneJournal(t *testing.T) {
	ctx, pool := openMessages(t)
	repository := postgres.NewMessages(pool)
	updates := postgres.NewUpdates(pool)
	now := time.Unix(100, 0).UTC()

	save := func() {
		t.Helper()

		err := postgres.NewProfiles(pool).Save(ctx, profile.Profile{
			Subject:   "anna",
			Fields:    profile.Fields{FirstName: "Анна"},
			UpdatedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	save()

	chat, err := repository.OpenDirect(ctx, "one", "anna", "petro", now)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := repository.Send(ctx, message(chat, 10, "petro", "x", "hi")); err != nil {
		t.Fatal(err)
	}

	save()

	got, err := updates.After(ctx, "anna", 0, 10)
	if err != nil || len(got) != 3 {
		t.Fatalf("got %+v, %v", got, err)
	}

	if got[0].Kind != update.KindProfile || got[1].Kind != update.KindMessage || got[2].Kind != update.KindProfile {
		t.Fatalf("kinds %+v", got)
	}

	if got[0].Profile.Fields.FirstName != "Анна" || got[1].Message.Text != "hi" || got[2].Seq != 3 {
		t.Fatalf("got %+v", got)
	}
}

func TestSendingBothWaysInParallelLeavesNoGaps(t *testing.T) {
	ctx, pool := openMessages(t)
	repository := postgres.NewMessages(pool)
	updates := postgres.NewUpdates(pool)

	chat, err := repository.OpenDirect(ctx, "one", "anna", "petro", time.Unix(100, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}

	const perPerson = 40

	var group sync.WaitGroup

	for offset, sender := range []string{"anna", "petro"} {
		group.Go(func() {
			for i := range perPerson {
				id := int64(offset*1000 + i + 1)
				if _, _, _, err := repository.Send(ctx, message(chat, id, sender, fmt.Sprint(i), "hi")); err != nil {
					t.Error(err)

					return
				}
			}
		})
	}

	group.Wait()

	for _, subject := range []string{"anna", "petro"} {
		got, err := updates.After(ctx, subject, 0, perPerson*2+1)
		if err != nil || len(got) != perPerson*2 {
			t.Fatalf("%s: %d, %v", subject, len(got), err)
		}

		for index, next := range got {
			if next.Seq != uint64(index+1) {
				t.Fatalf("%s: update %d has number %d", subject, index, next.Seq)
			}
		}
	}
}
