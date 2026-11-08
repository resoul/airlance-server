//go:build integration

package postgres_test

import (
	"errors"
	"testing"

	domain "go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/update"
	"go.airlance.org/server/internal/repository/postgres"
)

func TestReadPointersGiveUnreadCountsAndTicks(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "anna", "petro")

	chat, err := s.messages.OpenDirect(s.ctx, person("anna"), "petro")
	if err != nil {
		t.Fatal(err)
	}

	var last domain.Message

	for _, text := range []string{"раз", "два", "три"} {
		last, err = s.messages.Send(s.ctx, person("anna"), chat.ID, "id-"+text, text)
		if err != nil {
			t.Fatal(err)
		}
	}

	chats, err := s.messages.Chats(s.ctx, person("petro"), 0)
	if err != nil || chats[0].Unread != 3 {
		t.Fatalf("got %+v, %v", chats, err)
	}

	if own, _ := s.messages.Chats(s.ctx, person("anna"), 0); own[0].Unread != 0 {
		t.Fatalf("my own messages are unread for me: %+v", own)
	}

	state, err := s.messages.SetRead(s.ctx, person("petro"), chat.ID, last.ID-1)
	if err != nil || state.Unread != 1 || state.MyReadID != last.ID-1 {
		t.Fatalf("got %+v, %v", state, err)
	}

	back, err := s.messages.SetRead(s.ctx, person("petro"), chat.ID, 1)
	if err != nil || back.MyReadID != last.ID-1 {
		t.Fatalf("the pointer moved back: %+v, %v", back, err)
	}

	beyond, err := s.messages.SetRead(s.ctx, person("petro"), chat.ID, last.ID+1000)
	if err != nil || beyond.MyReadID != last.ID || beyond.Unread != 0 {
		t.Fatalf("got %+v, %v", beyond, err)
	}

	journal, err := postgres.NewUpdates(s.pool).After(s.ctx, "anna", 0, 100)
	if err != nil {
		t.Fatal(err)
	}

	tail := journal[len(journal)-1]
	if tail.Kind != update.KindRead || tail.Read.PeerReadID != last.ID {
		t.Fatalf("the author did not learn it was read: %+v", tail)
	}

	if _, err := s.messages.SetRead(s.ctx, person("stranger"), chat.ID, 1); !errors.Is(err, domain.ErrChatNotFound) {
		t.Fatalf("a stranger marked a chat read: %v", err)
	}
}

func TestReactionsAreOnePerPersonAndShownToTheChat(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "anna", "petro", "olena")
	chat := s.group(t, "anna", "petro", "olena")

	msg, err := s.messages.Send(s.ctx, person("anna"), chat.ID, "one", "привет")
	if err != nil {
		t.Fatal(err)
	}

	for who, emoji := range map[string]string{"petro": "👍", "olena": "👍"} {
		if _, err := s.messages.SetReaction(s.ctx, person(who), chat.ID, msg.ID, emoji); err != nil {
			t.Fatal(err)
		}
	}

	swapped, err := s.messages.SetReaction(s.ctx, person("petro"), chat.ID, msg.ID, "🔥")
	if err != nil || swapped.MyReaction != "🔥" || len(swapped.Reactions) != 2 {
		t.Fatalf("got %+v, %v", swapped, err)
	}

	if _, err := s.messages.SetReaction(s.ctx, person("petro"), chat.ID, msg.ID, "a b"); !errors.Is(err, domain.ErrInvalidReaction) {
		t.Fatalf("a bad reaction: %v", err)
	}

	seen := s.history(t, "olena", chat.ID)
	if len(seen) != 1 || seen[0].MyReaction != "👍" || seen[0].Reactions[0].Emoji != "👍" {
		t.Fatalf("got %+v", seen)
	}

	cleared, err := s.messages.SetReaction(s.ctx, person("olena"), chat.ID, msg.ID, "")
	if err != nil || cleared.MyReaction != "" || len(cleared.Reactions) != 1 {
		t.Fatalf("got %+v, %v", cleared, err)
	}

	journal, err := postgres.NewUpdates(s.pool).After(s.ctx, "anna", 0, 100)
	if err != nil {
		t.Fatal(err)
	}

	tail := journal[len(journal)-1]
	if tail.Kind != update.KindMessage || len(tail.Message.Reactions) != 1 || tail.Message.Reactions[0].Emoji != "🔥" {
		t.Fatalf("the author was not told: %+v", tail)
	}

	if _, err := s.messages.SetReaction(s.ctx, person("stranger"), chat.ID, msg.ID, "👍"); !errors.Is(err, domain.ErrChatNotFound) {
		t.Fatalf("a stranger reacted: %v", err)
	}
}

func TestDeliveredPointerReachesTheSenderOnlyInDirectChats(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "anna", "petro")

	chat, err := s.messages.OpenDirect(s.ctx, person("anna"), "petro")
	if err != nil {
		t.Fatal(err)
	}

	first, err := s.messages.Send(s.ctx, person("anna"), chat.ID, "one", "раз")
	if err != nil {
		t.Fatal(err)
	}

	second, err := s.messages.Send(s.ctx, person("anna"), chat.ID, "two", "два")
	if err != nil {
		t.Fatal(err)
	}

	if own, _ := s.messages.Chats(s.ctx, person("anna"), 0); own[0].PeerDeliveredID != 0 {
		t.Fatalf("delivered before the peer asked: %+v", own[0])
	}

	if _, err := s.messages.SetDelivered(s.ctx, person("petro"), chat.ID, first.ID); err != nil {
		t.Fatal(err)
	}

	state, err := s.messages.SetDelivered(s.ctx, person("anna"), chat.ID, second.ID)
	if err != nil || state.PeerDeliveredID != first.ID {
		t.Fatalf("got %+v, %v", state, err)
	}

	if own, _ := s.messages.Chats(s.ctx, person("anna"), 0); own[0].PeerDeliveredID != first.ID {
		t.Fatalf("got %+v", own[0])
	}

	if _, err := s.messages.SetRead(s.ctx, person("petro"), chat.ID, second.ID); err != nil {
		t.Fatal(err)
	}

	if own, _ := s.messages.Chats(s.ctx, person("anna"), 0); own[0].PeerDeliveredID != second.ID {
		t.Fatalf("reading must imply delivery: %+v", own[0])
	}

	group := s.group(t, "anna", "petro")
	if _, err := s.messages.SetDelivered(s.ctx, person("petro"), group.ID, 5); err != nil {
		t.Fatalf("a group must ignore it quietly: %v", err)
	}
}
