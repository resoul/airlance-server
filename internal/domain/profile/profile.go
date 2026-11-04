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
	minUsernameRunes = 5
	maxUsernameRunes = 32
)

var (
	ErrNotFound        = errors.New("profile not found")
	ErrInvalidName     = errors.New("invalid name")
	ErrInvalidUsername = errors.New("invalid username")
	ErrInvalidBio      = errors.New("invalid bio")
	ErrUsernameTaken   = errors.New("username taken")
)

type Fields struct {
	FirstName string
	LastName  string
	Username  string
	Bio       string
}

type Profile struct {
	Subject   string
	Fields    Fields
	UpdatedAt time.Time
}

func New(subject string, fields Fields, now time.Time) (Profile, error) {
	clean := Fields{
		FirstName: strings.TrimSpace(fields.FirstName),
		LastName:  strings.TrimSpace(fields.LastName),
		Username:  strings.TrimSpace(fields.Username),
		Bio:       strings.TrimSpace(fields.Bio),
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
