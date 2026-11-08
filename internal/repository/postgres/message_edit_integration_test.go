//go:build integration

package postgres_test

import (
	"errors"
	"testing"

	domain "go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/update"
	"go.airlance.org/server/internal/repository/postgres"
)

func TestEditingAndDeleting(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "anna", "petro")
	chat := s.group(t, "anna", "petro")

	first, err := s.messages.Send(s.ctx, person("anna"), chat.ID, "one", "привет")
	if err != nil {
		t.Fatal(err)
	}

	reply, err := s.messages.SendContent(s.ctx, person("petro"), chat.ID, "two", domain.Outgoing{
		Text: "ответ", ReplyToID: first.ID,
		Entities: []domain.Entity{{Kind: domain.EntityBold, Offset: 0, Length: 5}},
	})
	if err != nil || reply.ReplyToID != first.ID {
		t.Fatalf("got %+v, %v", reply, err)
	}

	_, err = s.messages.SendContent(s.ctx, person("petro"), chat.ID, "bad", domain.Outgoing{Text: "x", ReplyToID: 1})
	if !errors.Is(err, domain.ErrInvalidReply) {
		t.Fatalf("a reply to a missing message: %v", err)
	}

	edited, err := s.messages.Edit(s.ctx, person("anna"), chat.ID, first.ID, "исправлено", nil)
	if err != nil || edited.Text != "исправлено" || edited.EditedAt.IsZero() {
		t.Fatalf("got %+v, %v", edited, err)
	}

	if _, err := s.messages.Edit(s.ctx, person("petro"), chat.ID, first.ID, "чужое", nil); !errors.Is(err, domain.ErrNothingToEdit) {
		t.Fatalf("another person edited: %v", err)
	}

	if _, err := s.messages.Delete(s.ctx, person("petro"), chat.ID, []int64{first.ID}, false); err != nil {
		t.Fatal(err)
	}

	if got := s.history(t, "petro", chat.ID); len(got) != 1 || got[0].ID != reply.ID {
		t.Fatalf("a message deleted for me is still there: %+v", got)
	}

	if got := s.history(t, "anna", chat.ID); len(got) != 2 {
		t.Fatalf("deleting for me touched the other person: %+v", got)
	}

	if _, err := s.messages.Delete(s.ctx, person("petro"), chat.ID, []int64{first.ID}, true); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a member deleted another person's message for everyone: %v", err)
	}

	if _, err := s.messages.Delete(s.ctx, person("anna"), chat.ID, []int64{first.ID}, true); err != nil {
		t.Fatal(err)
	}

	if got := s.history(t, "anna", chat.ID); len(got) != 1 {
		t.Fatalf("got %+v", got)
	}

	journal, err := postgres.NewUpdates(s.pool).After(s.ctx, "petro", 0, 100)
	if err != nil {
		t.Fatal(err)
	}

	gone := 0

	for _, next := range journal {
		if next.Kind == update.KindGone && next.Message.ID == first.ID {
			gone++
		}
	}

	if gone == 0 {
		t.Fatalf("the other device was not told: %+v", journal)
	}
}

func TestLocationAndContactAreStoredAndSeenByOthers(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "anna", "petro")
	chat := s.group(t, "anna", "petro")

	_, err := s.messages.SendContent(s.ctx, person("anna"), chat.ID, "place", domain.Outgoing{
		Attachments: []domain.Attachment{{Kind: domain.AttachmentLocation, Latitude: 50.45, Longitude: 30.52, Title: "Київ"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.messages.SendContent(s.ctx, person("anna"), chat.ID, "card", domain.Outgoing{
		Attachments: []domain.Attachment{{Kind: domain.AttachmentContact, Name: "Олена", Phone: "+380501112233"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	seen := s.history(t, "petro", chat.ID)
	if len(seen) != 2 {
		t.Fatalf("got %+v", seen)
	}

	card, place := seen[0].Attachments[0], seen[1].Attachments[0]
	if card.Phone != "+380501112233" || card.Name != "Олена" || place.Latitude != 50.45 || place.Title != "Київ" {
		t.Fatalf("got %+v and %+v", card, place)
	}
}
