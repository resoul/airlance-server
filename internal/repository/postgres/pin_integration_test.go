//go:build integration

package postgres_test

import (
	"errors"
	"fmt"
	"testing"

	domain "go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/update"
	"go.airlance.org/server/internal/repository/postgres"
	usecase "go.airlance.org/server/internal/usecase/message"
)

func TestAPinIsPersonal(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "anna", "petro", "stranger")
	chat := s.group(t, "anna", "petro")

	pinned, err := s.messages.SetPinned(s.ctx, person("anna"), chat.ID, true)
	if err != nil || !pinned.IsPinned {
		t.Fatalf("got %+v, %v", pinned, err)
	}

	mine, err := s.messages.Chats(s.ctx, person("anna"), 0)
	if err != nil || !mine[0].IsPinned {
		t.Fatalf("got %+v, %v", mine, err)
	}

	theirs, err := s.messages.Chats(s.ctx, person("petro"), 0)
	if err != nil || theirs[0].IsPinned {
		t.Fatalf("another member sees my pin: %+v, %v", theirs, err)
	}

	journal, err := postgres.NewUpdates(s.pool).After(s.ctx, "anna", 0, 10)
	if err != nil || journal[len(journal)-1].Kind != update.KindChats {
		t.Fatalf("my other devices were not told: %+v, %v", journal, err)
	}

	if _, err := s.messages.SetPinned(s.ctx, person("stranger"), chat.ID, true); !errors.Is(err, domain.ErrChatNotFound) {
		t.Fatalf("a stranger pinned: %v", err)
	}

	unpinned, err := s.messages.SetPinned(s.ctx, person("anna"), chat.ID, false)
	if err != nil || unpinned.IsPinned {
		t.Fatalf("got %+v, %v", unpinned, err)
	}
}

func TestPinsHaveALimit(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "anna")

	var first string

	for index := range domain.MaxPinnedChats + 1 {
		chat, err := s.messages.Create(s.ctx, person("anna"), usecase.NewChat{
			ClientID: fmt.Sprintf("pin-%d", index), Kind: domain.KindGroup, Title: "Группа",
		})
		if err != nil {
			t.Fatal(err)
		}

		if index == 0 {
			first = chat.ID
		}

		_, err = s.messages.SetPinned(s.ctx, person("anna"), chat.ID, true)
		if index < domain.MaxPinnedChats && err != nil {
			t.Fatal(err)
		}

		if index == domain.MaxPinnedChats && !errors.Is(err, domain.ErrTooManyPinned) {
			t.Fatalf("over the limit: %v", err)
		}
	}

	if _, err := s.messages.SetPinned(s.ctx, person("anna"), first, true); err != nil {
		t.Fatalf("a repeat is not one more: %v", err)
	}
}
