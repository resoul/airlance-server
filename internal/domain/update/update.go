package update

import (
	"errors"
	"math"

	"go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/profile"
	"go.airlance.org/server/internal/domain/settings"
)

const MaxSeq uint64 = math.MaxInt64

var ErrInvalidSeq = errors.New("invalid update number")

type Kind int16

const (
	KindProfile   Kind = 1
	KindMessage   Kind = 2
	KindStories   Kind = 3
	KindContacts  Kind = 4
	KindChats     Kind = 5
	KindScheduled Kind = 6
	KindFolders   Kind = 7
	KindGone      Kind = 8
	KindPins      Kind = 9
	KindRead      Kind = 10
	KindDraft     Kind = 11
	KindSettings  Kind = 12
)

type Update struct {
	Seq     uint64
	Kind    Kind
	Profile profile.Profile
	Message message.Message
	Pins    []int64
	Read    message.ReadState
	Draft   message.DraftState
	Setting settings.Record
}
