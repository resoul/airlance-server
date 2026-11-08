package settings

import (
	"errors"
	"regexp"
	"time"
	"unicode/utf8"
)

const (
	MaxDataSize = 64 * 1024
	MaxKeyLen   = 64
	MaxNameLen  = 64
)

var (
	ErrInvalid  = errors.New("invalid settings")
	ErrNotFound = errors.New("wallpaper not found")

	keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_.]*$`)
)

type Record struct {
	Key       string
	Version   int64
	Data      []byte
	UpdatedAt time.Time
}

type Wallpaper struct {
	ID      string
	Name    string
	MediaID string
	Sort    int32
}

func CheckKey(key string) error {
	if len(key) > MaxKeyLen || !keyPattern.MatchString(key) {
		return ErrInvalid
	}

	return nil
}

func CheckData(data []byte) error {
	if len(data) > MaxDataSize {
		return ErrInvalid
	}

	return nil
}

func CheckWallpaper(next Wallpaper) error {
	if next.ID == "" || next.MediaID == "" || next.Name == "" || utf8.RuneCountInString(next.Name) > MaxNameLen {
		return ErrInvalid
	}

	return nil
}
