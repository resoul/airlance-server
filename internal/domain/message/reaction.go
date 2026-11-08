package message

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxReactionBytes = 32

var (
	ErrInvalidReaction = errors.New("invalid reaction")
	ErrInvalidRead     = errors.New("invalid read mark")
)

type ReactionCount struct {
	Emoji string
	Count int32
}

type ReadState struct {
	ChatID     string
	MyReadID   int64
	PeerReadID int64
	Unread     int32

	PeerDeliveredID int64
}

type DraftState struct {
	ChatID      string
	Text        string
	UpdatedAtMs int64
}

func CheckDraft(text string) (string, error) {
	if !utf8.ValidString(text) || utf8.RuneCountInString(text) > MaxTextLength {
		return "", ErrInvalidText
	}

	if strings.TrimSpace(text) == "" {
		return "", nil
	}

	return text, nil
}

func CheckReaction(emoji string) error {
	if emoji == "" {
		return nil
	}

	if len(emoji) > MaxReactionBytes || !utf8.ValidString(emoji) {
		return ErrInvalidReaction
	}

	for _, next := range emoji {
		if unicode.IsControl(next) || unicode.IsSpace(next) {
			return ErrInvalidReaction
		}
	}

	return nil
}
