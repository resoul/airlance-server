package message

import (
	"errors"
	"math"
	"strings"
	"time"
	"unicode"
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
	AttachmentLocation  AttachmentKind = 7
	AttachmentContact   AttachmentKind = 8
)

const (
	MaxAttachments    = 10
	MaxMediaIDLength  = 64
	MaxFileNameLength = 255
	MaxTagLength      = 128
	MaxWaveformLength = 256
	MaxPhoneLength    = 32
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
	Latitude     float64
	Longitude    float64
	Phone        string
}

type Outgoing struct {
	Text        string
	ForwardName string
	Attachments []Attachment
	ReplyToID   int64
	Entities    []Entity
	ThreadID    int64
	Poll        *Poll
}

func Compose(chatID string, id int64, sender, clientID string, outgoing Outgoing, now time.Time) (Message, error) {
	if outgoing.ReplyToID < 0 {
		return Message{}, ErrInvalidReply
	}

	if outgoing.ThreadID < 0 {
		return Message{}, ErrInvalidThread
	}

	if err := checkEntities(outgoing.Entities); err != nil {
		return Message{}, err
	}

	next, err := compose(chatID, id, sender, clientID, outgoing, now)
	if err != nil {
		return Message{}, err
	}

	next.ReplyToID, next.Entities, next.ThreadID = outgoing.ReplyToID, outgoing.Entities, outgoing.ThreadID

	return next, nil
}

func compose(chatID string, id int64, sender, clientID string, outgoing Outgoing, now time.Time) (Message, error) {
	if outgoing.Poll != nil {
		return composePoll(chatID, id, sender, clientID, outgoing, now)
	}

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
	if a.Kind < AttachmentPhoto || a.Kind > AttachmentContact {
		return false
	}

	if a.Kind == AttachmentLocation || a.Kind == AttachmentContact {
		return a.isValidCard()
	}

	if !isMediaID(a.MediaID) || (a.ThumbMediaID != "" && !isMediaID(a.ThumbMediaID)) {
		return false
	}

	if a.Size < 0 || a.Width < 0 || a.Height < 0 || a.DurationMs < 0 || len(a.Waveform) > MaxWaveformLength {
		return false
	}

	return isShort(a.Name, MaxFileNameLength) && isShort(a.Title, MaxTagLength) && isShort(a.Performer, MaxTagLength)
}

func (a Attachment) isValidCard() bool {
	if a.MediaID != "" || a.ThumbMediaID != "" || !isShort(a.Name, MaxFileNameLength) || !isShort(a.Title, MaxTagLength) {
		return false
	}

	if a.Kind == AttachmentLocation {
		return !math.IsNaN(a.Latitude) && !math.IsNaN(a.Longitude) &&
			a.Latitude >= -90 && a.Latitude <= 90 && a.Longitude >= -180 && a.Longitude <= 180
	}

	return strings.TrimSpace(a.Name) != "" && a.Phone != "" && isShort(a.Phone, MaxPhoneLength) &&
		strings.IndexFunc(a.Phone, unicode.IsControl) < 0
}

func isMediaID(id string) bool {
	return id != "" && len(id) <= MaxMediaIDLength && utf8.ValidString(id)
}

func isShort(text string, limit int) bool {
	return utf8.ValidString(text) && utf8.RuneCountInString(text) <= limit
}

func (m Message) NeedsMedia() bool {
	return len(m.Attachments) > 0 || m.Poll != nil
}

func (m Message) MediaIDs() []string {
	ids := make([]string, 0, len(m.Attachments))

	for _, next := range m.Attachments {
		if next.MediaID == "" {
			continue
		}

		ids = append(ids, next.MediaID)
		if next.ThumbMediaID != "" {
			ids = append(ids, next.ThumbMediaID)
		}
	}

	return ids
}

func composePoll(chatID string, id int64, sender, clientID string, outgoing Outgoing, now time.Time) (Message, error) {
	if len(outgoing.Attachments) != 0 || outgoing.ForwardName != "" {
		return Message{}, ErrInvalidPoll
	}

	poll, err := NewPoll(*outgoing.Poll, now)
	if err != nil {
		return Message{}, err
	}

	next, err := New(chatID, id, sender, clientID, poll.Question, now)
	if err != nil {
		return Message{}, err
	}

	next.Poll = &poll

	return next, nil
}
