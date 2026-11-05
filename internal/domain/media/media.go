package media

import (
	"errors"
	"time"
)

const MaxSize int64 = 2 << 30

var (
	ErrInvalidSize = errors.New("invalid file size")
	ErrNotFound    = errors.New("media not found")
)

type Object struct {
	ID        string
	Owner     string
	Size      int64
	CreatedAt time.Time
}

func New(id, owner string, size int64, now time.Time) (Object, error) {
	if size <= 0 || size > MaxSize {
		return Object{}, ErrInvalidSize
	}

	return Object{ID: id, Owner: owner, Size: size, CreatedAt: now}, nil
}
