package update

import (
	"errors"
	"math"

	"go.airlance.org/server/internal/domain/profile"
)

const MaxSeq uint64 = math.MaxInt64

var ErrInvalidSeq = errors.New("invalid update number")

type Kind int16

const KindProfile Kind = 1

type Update struct {
	Seq     uint64
	Kind    Kind
	Profile profile.Profile
}
