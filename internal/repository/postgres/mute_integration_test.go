//go:build integration

package postgres_test

import (
	"errors"
	"testing"

	domain "go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/update"
	"go.airlance.org/server/internal/repository/postgres"
)

func TestMutingIsPersonalAndKeepsMessagesComing(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "anna", "petro", "stranger")
	chat := s.group(t, "anna", "petro")

	muted, err := s.messages.SetMuted(s.ctx, person("anna"), chat.ID, true)
	if err != nil || !muted.IsMuted {
		t.Fatalf("got %+v, %v", muted, err)
	}

	theirs, err := s.messages.Chats(s.ctx, person("petro"), 0)
	if err != nil || theirs[0].IsMuted {
		t.Fatalf("another member sees my setting: %+v, %v", theirs, err)
	}

	journal := postgres.NewUpdates(s.pool)

	last, err := journal.After(s.ctx, "anna", 0, 100)
	if err != nil || last[len(last)-1].Kind != update.KindChats {
		t.Fatalf("my other devices were not told: %+v, %v", last, err)
	}

	before, err := journal.Latest(s.ctx, "anna")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.messages.Send(s.ctx, person("petro"), chat.ID, "m1", "привет"); err != nil {
		t.Fatal(err)
	}

	after, err := journal.After(s.ctx, "anna", before, 10)
	if err != nil || len(after) != 1 || after[0].Kind != update.KindMessage {
		t.Fatalf("a muted chat must still get messages: %+v, %v", after, err)
	}

	if _, err := s.messages.SetMuted(s.ctx, person("stranger"), chat.ID, true); !errors.Is(err, domain.ErrChatNotFound) {
		t.Fatalf("a stranger muted: %v", err)
	}

	unmuted, err := s.messages.SetMuted(s.ctx, person("anna"), chat.ID, false)
	if err != nil || unmuted.IsMuted {
		t.Fatalf("got %+v, %v", unmuted, err)
	}
}
