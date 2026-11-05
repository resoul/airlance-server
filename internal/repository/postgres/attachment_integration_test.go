//go:build integration

package postgres_test

import (
	"errors"
	"testing"
	"time"

	"go.airlance.org/server/internal/domain/media"
	domain "go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/repository/postgres"
	usecase "go.airlance.org/server/internal/usecase/message"
)

func TestAPhotoReachesMembersAndOnlyThem(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "anna", "petro", "stranger")
	chat := s.group(t, "anna", "petro")
	files := postgres.NewMedia(s.pool)

	for _, id := range []string{"photo1", "thumb1"} {
		object, err := media.New(id, "anna", 100, time.Now())
		if err != nil {
			t.Fatal(err)
		}

		if err := files.Create(s.ctx, object); err != nil {
			t.Fatal(err)
		}
	}

	attachment := domain.Attachment{
		Kind: domain.AttachmentPhoto, MediaID: "photo1", ThumbMediaID: "thumb1", Width: 640, Height: 480, Waveform: []byte{1, 2},
	}

	sent, err := s.messages.SendContent(s.ctx, person("anna"), chat.ID, "m1", domain.Outgoing{
		Text: "подпись", ForwardName: "Канал", Attachments: []domain.Attachment{attachment},
	})
	if err != nil {
		t.Fatal(err)
	}

	again, err := s.messages.SendContent(s.ctx, person("anna"), chat.ID, "m1", domain.Outgoing{
		Text: "подпись", Attachments: []domain.Attachment{attachment},
	})
	if err != nil || again.ID != sent.ID || len(again.Attachments) != 1 || again.ForwardName != "Канал" {
		t.Fatalf("a repeat lost the content: %+v, %v", again, err)
	}

	got := s.history(t, "petro", chat.ID)
	if len(got) != 1 || got[0].ForwardName != "Канал" || len(got[0].Attachments) != 1 {
		t.Fatalf("got %+v", got)
	}

	if got[0].Attachments[0].ThumbMediaID != "thumb1" || string(got[0].Attachments[0].Waveform) != "\x01\x02" {
		t.Fatalf("got %+v", got[0].Attachments[0])
	}

	for viewer, want := range map[string]bool{"petro": true, "stranger": false} {
		found, err := files.IsChatMedia(s.ctx, "photo1", viewer)
		if err != nil || found != want {
			t.Errorf("%s: %v, %v", viewer, found, err)
		}
	}

	_, err = s.messages.SendContent(s.ctx, person("petro"), chat.ID, "m2", domain.Outgoing{
		Attachments: []domain.Attachment{{Kind: domain.AttachmentPhoto, MediaID: "photo1"}},
	})
	if !errors.Is(err, domain.ErrInvalidAttachment) {
		t.Fatalf("someone else's file was sent: %v", err)
	}

	if err := s.messages.Leave(s.ctx, person("petro"), chat.ID); err != nil {
		t.Fatal(err)
	}

	if found, err := files.IsChatMedia(s.ctx, "photo1", "petro"); err != nil || found {
		t.Fatalf("a former member keeps the file: %v, %v", found, err)
	}
}

func TestAPublicChatsFileIsOpenToReaders(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "anna", "guest")
	files := postgres.NewMedia(s.pool)

	chat, err := s.messages.Create(s.ctx, person("anna"), usecase.NewChat{
		ClientID: "c", Kind: domain.KindChannel, Title: "Открытый", Link: "open_media",
	})
	if err != nil {
		t.Fatal(err)
	}

	object, err := media.New("public1", "anna", 100, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if err := files.Create(s.ctx, object); err != nil {
		t.Fatal(err)
	}

	_, err = s.messages.SendContent(s.ctx, person("anna"), chat.ID, "m1", domain.Outgoing{
		Attachments: []domain.Attachment{{Kind: domain.AttachmentFile, MediaID: "public1", Name: "отчёт.pdf"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if found, err := files.IsChatMedia(s.ctx, "public1", "guest"); err != nil || !found {
		t.Fatalf("got %v, %v", found, err)
	}
}
