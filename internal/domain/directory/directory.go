package directory

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	MinQueryLength = 3
	MaxQueryLength = 64
	MaxResults     = 20
)

var (
	ErrInvalidQuery      = errors.New("invalid search query")
	ErrTooManySearches   = errors.New("too many searches")
	ErrSearchUnavailable = errors.New("search unavailable")
)

type Person struct {
	Subject      string
	FirstName    string
	LastName     string
	Username     string
	PhotoMediaID string
	VideoMediaID string
	IsHidden     bool
}

func NormalizeQuery(raw string) (string, error) {
	query := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "@"))
	length := utf8.RuneCountInString(query)

	if !utf8.ValidString(query) || length < MinQueryLength || length > MaxQueryLength {
		return "", ErrInvalidQuery
	}

	return query, nil
}
