package contact

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxSyncPhones = 5000
	maxNameRunes  = 64
	minPhone      = 7
	maxPhone      = 15
)

var (
	ErrNotFound      = errors.New("contact not found")
	ErrInvalidPeer   = errors.New("invalid contact")
	ErrInvalidName   = errors.New("invalid contact name")
	ErrInvalidPhone  = errors.New("invalid phone")
	ErrTooManyPhones = errors.New("too many phones")
	ErrTooManyTries  = errors.New("too many contact lookups")
)

type Contact struct {
	Subject         string
	FirstName       string
	LastName        string
	Username        string
	PhotoMediaID    string
	VideoMediaID    string
	CustomFirstName string
	CustomLastName  string
	Phone           string
	FromDevice      bool
}

func Digits(raw string) string {
	var digits strings.Builder

	for _, char := range raw {
		if char >= '0' && char <= '9' {
			digits.WriteRune(char)
		}
	}

	return digits.String()
}

func NormalizePhone(raw string) (string, error) {
	digits := Digits(raw)
	if len(digits) < minPhone || len(digits) > maxPhone {
		return "", ErrInvalidPhone
	}

	return digits, nil
}

func CleanName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > maxNameRunes {
		return "", ErrInvalidName
	}

	if strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", ErrInvalidName
	}

	return name, nil
}
