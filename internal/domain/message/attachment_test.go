package message_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"go.airlance.org/server/internal/domain/message"
)

func photo() message.Attachment {
	return message.Attachment{Kind: message.AttachmentPhoto, MediaID: "m1", ThumbMediaID: "t1", Width: 10, Height: 10}
}

func compose(outgoing message.Outgoing) (message.Message, error) {
	return message.Compose("c", 1, "anna", "l", outgoing, time.Unix(1, 0))
}

func TestAnAttachmentMayGoWithoutText(t *testing.T) {
	t.Parallel()

	next, err := compose(message.Outgoing{Attachments: []message.Attachment{photo()}})
	if err != nil || next.Text != "" {
		t.Fatalf("got %+v, %v", next, err)
	}

	if !slices.Equal(next.MediaIDs(), []string{"m1", "t1"}) {
		t.Fatalf("got %v", next.MediaIDs())
	}

	if _, err := compose(message.Outgoing{Text: "  "}); !errors.Is(err, message.ErrInvalidText) {
		t.Fatalf("an empty message passed: %v", err)
	}
}

func TestBadAttachmentsAreRefused(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*message.Attachment){
		"a kind":     func(a *message.Attachment) { a.Kind = 99 },
		"no media":   func(a *message.Attachment) { a.MediaID = "" },
		"a long id":  func(a *message.Attachment) { a.ThumbMediaID = strings.Repeat("x", 65) },
		"a size":     func(a *message.Attachment) { a.Size = -1 },
		"a name":     func(a *message.Attachment) { a.Name = strings.Repeat("я", 256) },
		"a waveform": func(a *message.Attachment) { a.Waveform = make([]byte, 257) },
	}

	for name, change := range cases {
		next := photo()
		change(&next)

		if _, err := compose(message.Outgoing{Attachments: []message.Attachment{next}}); !errors.Is(err, message.ErrInvalidAttachment) {
			t.Errorf("%s: got %v", name, err)
		}
	}

	many := make([]message.Attachment, message.MaxAttachments+1)
	for index := range many {
		many[index] = photo()
	}

	if _, err := compose(message.Outgoing{Attachments: many}); !errors.Is(err, message.ErrInvalidAttachment) {
		t.Fatalf("too many: %v", err)
	}
}

func TestAForwardKeepsOnlyAName(t *testing.T) {
	t.Parallel()

	next, err := compose(message.Outgoing{Text: "новость", ForwardName: " Канал "})
	if err != nil || next.ForwardName != "Канал" {
		t.Fatalf("got %+v, %v", next, err)
	}
}
