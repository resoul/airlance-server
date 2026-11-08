package message

import (
	"errors"
	"time"
	"unicode/utf8"
)

type EntityKind int32

const (
	EntitySpoiler       EntityKind = 1
	EntityMention       EntityKind = 2
	EntityBold          EntityKind = 3
	EntityItalic        EntityKind = 4
	EntityStrikethrough EntityKind = 5
	EntityCode          EntityKind = 6
	EntityPre           EntityKind = 7
	EntityQuote         EntityKind = 8
)

const (
	MaxEntities     = 256
	MaxEntityUserID = 128
	EditWindow      = 48 * time.Hour
	MaxDeleteBatch  = 100
)

var (
	ErrInvalidEntity  = errors.New("invalid entity")
	ErrInvalidReply   = errors.New("invalid reply")
	ErrMessageMissing = errors.New("message not found")
	ErrEditExpired    = errors.New("edit window is over")
	ErrNothingToEdit  = errors.New("message cannot be edited")
	ErrInvalidDelete  = errors.New("invalid delete")
)

type Entity struct {
	Kind   EntityKind
	Offset int32
	Length int32
	UserID string
}

func checkEntities(entities []Entity) error {
	if len(entities) > MaxEntities {
		return ErrInvalidEntity
	}

	for _, next := range entities {
		if next.Kind < EntitySpoiler || next.Kind > EntityQuote || next.Offset < 0 || next.Length <= 0 {
			return ErrInvalidEntity
		}

		if !utf8.ValidString(next.UserID) || len(next.UserID) > MaxEntityUserID {
			return ErrInvalidEntity
		}

		if (next.Kind == EntityMention) != (next.UserID != "") {
			return ErrInvalidEntity
		}
	}

	return nil
}
