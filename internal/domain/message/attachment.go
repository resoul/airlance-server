package message

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

type AttachmentKind int32

const (
	AttachmentPhoto     AttachmentKind = 1
	AttachmentVideo     AttachmentKind = 2
	AttachmentVoice     AttachmentKind = 3
	AttachmentFile      AttachmentKind = 4
	AttachmentMusic     AttachmentKind = 5
	AttachmentVideoNote AttachmentKind = 6
)

const (
	MaxAttachments    = 10
	MaxMediaIDLength  = 64
	MaxFileNameLength = 255
	MaxTagLength      = 128
	MaxWaveformLength = 256
)

var ErrInvalidAttachment = errors.New("invalid attachment")

type Attachment struct {
	Kind         AttachmentKind
	MediaID      string
	ThumbMediaID string
	Name         string
	Size         int64
	Width        int32
	Height       int32
	DurationMs   int32
	Waveform     []byte
	Title        string
	Performer    string
}

type Outgoing struct {
	Text        string
	ForwardName string
	Attachments []Attachment
}

func Compose(chatID string, id int64, sender, clientID string, outgoing Outgoing, now time.Time) (Message, error) {
	if len(outgoing.Attachments) == 0 {
		next, err := New(chatID, id, sender, clientID, outgoing.Text, now)
		if err != nil {
			return Message{}, err
		}

		return next.withForward(outgoing.ForwardName)
	}

	if err := checkAttachments(outgoing.Attachments); err != nil {
		return Message{}, err
	}

	clean := strings.TrimSpace(outgoing.Text)
	if !utf8.ValidString(clean) || utf8.RuneCountInString(clean) > MaxTextLength {
		return Message{}, ErrInvalidText
	}

	if clientID == "" || len(clientID) > MaxClientIDLength || !utf8.ValidString(clientID) {
		return Message{}, ErrInvalidClientID
	}

	next := Message{
		ChatID: chatID, ID: id, Sender: sender, ClientID: clientID, Text: clean, SentAt: now,
		Attachments: outgoing.Attachments,
	}

	return next.withForward(outgoing.ForwardName)
}

func (m Message) withForward(name string) (Message, error) {
	clean := strings.TrimSpace(name)
	if !utf8.ValidString(clean) || utf8.RuneCountInString(clean) > MaxTagLength {
		return Message{}, ErrInvalidAttachment
	}

	m.ForwardName = clean

	return m, nil
}

func checkAttachments(attachments []Attachment) error {
	if len(attachments) > MaxAttachments {
		return ErrInvalidAttachment
	}

	for _, next := range attachments {
		if !next.isValid() {
			return ErrInvalidAttachment
		}
	}

	return nil
}

func (a Attachment) isValid() bool {
	if a.Kind < AttachmentPhoto || a.Kind > AttachmentVideoNote {
		return false
	}

	if !isMediaID(a.MediaID) || (a.ThumbMediaID != "" && !isMediaID(a.ThumbMediaID)) {
		return false
	}

	if a.Size < 0 || a.Width < 0 || a.Height < 0 || a.DurationMs < 0 || len(a.Waveform) > MaxWaveformLength {
		return false
	}

	return isShort(a.Name, MaxFileNameLength) && isShort(a.Title, MaxTagLength) && isShort(a.Performer, MaxTagLength)
}

func isMediaID(id string) bool {
	return id != "" && len(id) <= MaxMediaIDLength && utf8.ValidString(id)
}

func isShort(text string, limit int) bool {
	return utf8.ValidString(text) && utf8.RuneCountInString(text) <= limit
}

func (m Message) MediaIDs() []string {
	ids := make([]string, 0, len(m.Attachments))

	for _, next := range m.Attachments {
		ids = append(ids, next.MediaID)
		if next.ThumbMediaID != "" {
			ids = append(ids, next.ThumbMediaID)
		}
	}

	return ids
}
