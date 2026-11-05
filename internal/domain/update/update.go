package update

import (
	"errors"
	"math"

	"go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/profile"
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
)

type Update struct {
	Seq     uint64
	Kind    Kind
	Profile profile.Profile
	Message message.Message
}
