package message

import "errors"

const (
	MaxPins          = 20
	MaxStatsBatch    = 100
	MaxRecentAuthors = 3
)

var (
	ErrInvalidThread = errors.New("invalid thread")
	ErrTooManyPins   = errors.New("too many pinned messages")
	ErrInvalidStats  = errors.New("invalid stats request")
)

type Commenter struct {
	Subject      string
	FirstName    string
	LastName     string
	PhotoMediaID string
	VideoMediaID string
}

type PostStat struct {
	MessageID int64
	Views     int32
	Comments  int32
	Recent    []Commenter
}

func MayPin(access Access) bool {
	return access.Can(RightPin)
}
