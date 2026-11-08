//go:build integration

package postgres_test

import (
	"errors"
	"testing"

	domain "go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/update"
	"go.airlance.org/server/internal/repository/postgres"
)

func TestArchiveIsPersonal(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "anna", "petro", "stranger")
	chat := s.group(t, "anna", "petro")

	archived, err := s.messages.SetArchived(s.ctx, person("anna"), chat.ID, true)
	if err != nil || !archived.IsArchived {
		t.Fatalf("got %+v, %v", archived, err)
	}

	theirs, err := s.messages.Chats(s.ctx, person("petro"), 0)
	if err != nil || theirs[0].IsArchived {
		t.Fatalf("another member sees my archive: %+v, %v", theirs, err)
	}

	again, err := s.messages.SetArchived(s.ctx, person("anna"), chat.ID, true)
	if err != nil || !again.IsArchived {
		t.Fatalf("a repeat: %+v, %v", again, err)
	}

	if _, err := s.messages.SetArchived(s.ctx, person("stranger"), chat.ID, true); !errors.Is(err, domain.ErrChatNotFound) {
		t.Fatalf("a stranger archived: %v", err)
	}

	restored, err := s.messages.SetArchived(s.ctx, person("anna"), chat.ID, false)
	if err != nil || restored.IsArchived {
		t.Fatalf("got %+v, %v", restored, err)
	}
}

func TestDraftsArePersonalAndSyncedToMyDevices(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "anna", "petro")
	chat := s.group(t, "anna", "petro")

	state, err := s.messages.SetDraft(s.ctx, person("anna"), chat.ID, "недописано")
	if err != nil || state.Text != "недописано" || state.UpdatedAtMs == 0 {
		t.Fatalf("got %+v, %v", state, err)
	}

	mine, err := s.messages.Chats(s.ctx, person("anna"), 0)
	if err != nil || mine[0].Draft != "недописано" || mine[0].DraftAtMs == 0 {
		t.Fatalf("got %+v, %v", mine, err)
	}

	theirs, err := s.messages.Chats(s.ctx, person("petro"), 0)
	if err != nil || theirs[0].Draft != "" {
		t.Fatalf("another member reads my draft: %+v, %v", theirs, err)
	}

	journal, err := postgres.NewUpdates(s.pool).After(s.ctx, "anna", 0, 100)
	if err != nil {
		t.Fatal(err)
	}

	tail := journal[len(journal)-1]
	if tail.Kind != update.KindDraft || tail.Draft.Text != "недописано" {
		t.Fatalf("my other devices were not told: %+v", tail)
	}

	if _, err := s.messages.SetDraft(s.ctx, person("anna"), chat.ID, "недописано"); err != nil {
		t.Fatal(err)
	}

	if again, _ := postgres.NewUpdates(s.pool).After(s.ctx, "anna", 0, 100); len(again) != len(journal) {
		t.Fatalf("an unchanged draft wrote to the journal: %d rows against %d", len(again), len(journal))
	}

	cleared, err := s.messages.SetDraft(s.ctx, person("anna"), chat.ID, "  ")
	if err != nil || cleared.Text != "" {
		t.Fatalf("got %+v, %v", cleared, err)
	}

	if _, err := s.messages.SetDraft(s.ctx, person("stranger"), chat.ID, "x"); !errors.Is(err, domain.ErrChatNotFound) {
		t.Fatalf("a stranger wrote a draft: %v", err)
	}
}
