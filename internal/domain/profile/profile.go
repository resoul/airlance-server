package profile

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	ScopeRead  = "profile:read"
	ScopeWrite = "profile:write"

	maxNameRunes     = 64
	maxBioRunes      = 70
	maxPhoneRunes    = 32
	minUsernameRunes = 5
	maxUsernameRunes = 32

	MaxPhotos           = 100
	MaxPhotoBytes int64 = 10 << 20
	MaxVideoBytes int64 = 50 << 20
)

var (
	ErrNotFound        = errors.New("profile not found")
	ErrInvalidName     = errors.New("invalid name")
	ErrInvalidUsername = errors.New("invalid username")
	ErrInvalidBio      = errors.New("invalid bio")
	ErrInvalidPhone    = errors.New("invalid phone")
	ErrInvalidMedia    = errors.New("invalid profile media")
	ErrTooManyPhotos   = errors.New("too many profile photos")
	ErrUsernameTaken   = errors.New("username taken")
)

type Fields struct {
	FirstName    string
	LastName     string
	Username     string
	Bio          string
	Phone        string
	PhotoMediaID string
	VideoMediaID string
}

type Photo struct {
	PhotoMediaID string
	VideoMediaID string
	AddedAt      time.Time
}

type Profile struct {
	Subject   string
	Fields    Fields
	Photos    []Photo
	UpdatedAt time.Time
}

func NewPhoto(photoMediaID, videoMediaID string, now time.Time) (Photo, error) {
	if photoMediaID == "" {
		return Photo{}, ErrInvalidMedia
	}

	return Photo{PhotoMediaID: photoMediaID, VideoMediaID: videoMediaID, AddedAt: now.UTC()}, nil
}

func New(subject string, fields Fields, now time.Time) (Profile, error) {
	clean := Fields{
		FirstName:    strings.TrimSpace(fields.FirstName),
		LastName:     strings.TrimSpace(fields.LastName),
		Username:     strings.TrimSpace(fields.Username),
		Bio:          strings.TrimSpace(fields.Bio),
		Phone:        strings.TrimSpace(fields.Phone),
		PhotoMediaID: fields.PhotoMediaID,
		VideoMediaID: fields.VideoMediaID,
	}
	if !validName(clean.FirstName, true) || !validName(clean.LastName, false) {
		return Profile{}, ErrInvalidName
	}

	if clean.Username != "" && !validUsername(clean.Username) {
		return Profile{}, ErrInvalidUsername
	}

	if !utf8.ValidString(clean.Bio) || utf8.RuneCountInString(clean.Bio) > maxBioRunes {
		return Profile{}, ErrInvalidBio
	}

	if !validPhone(clean.Phone) {
		return Profile{}, ErrInvalidPhone
	}

	if clean.VideoMediaID != "" && clean.PhotoMediaID == "" {
		return Profile{}, ErrInvalidMedia
	}

	return Profile{Subject: subject, Fields: clean, UpdatedAt: now.UTC()}, nil
}

func validName(name string, required bool) bool {
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > maxNameRunes {
		return false
	}

	if required && name == "" {
		return false
	}

	return strings.IndexFunc(name, unicode.IsControl) < 0
}

func validPhone(phone string) bool {
	if !utf8.ValidString(phone) || utf8.RuneCountInString(phone) > maxPhoneRunes {
		return false
	}

	return strings.IndexFunc(phone, unicode.IsControl) < 0
}

func validUsername(username string) bool {
	if len(username) < minUsernameRunes || len(username) > maxUsernameRunes {
		return false
	}

	for i, char := range username {
		isLetter := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
		isDigit := char >= '0' && char <= '9'
		switch {
		case i == 0 && !isLetter:
			return false
		case !isLetter && !isDigit && char != '_':
			return false
		}
	}

	return !strings.HasSuffix(username, "_")
}
