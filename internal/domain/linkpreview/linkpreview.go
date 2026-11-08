package linkpreview

import (
	"errors"
	"net/url"
	"strings"
)

const (
	MaxURLLength = 2048
	MaxTitle     = 200
	MaxSummary   = 400
	MaxSite      = 80
	LargeWidth   = 400
)

var (
	ErrInvalidURL  = errors.New("invalid link")
	ErrRateLimited = errors.New("too many link previews")
)

type Preview struct {
	Found        bool
	URL          string
	Site         string
	Title        string
	Summary      string
	PhotoMediaID string
	Width        int32
	Height       int32
}

func (p Preview) HasLargePhoto() bool {
	return p.PhotoMediaID != "" && p.Width >= LargeWidth
}

func Normalize(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > MaxURLLength {
		return "", ErrInvalidURL
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return "", ErrInvalidURL
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", ErrInvalidURL
	}

	if port := parsed.Port(); port != "" && port != "80" && port != "443" {
		return "", ErrInvalidURL
	}

	parsed.Scheme = scheme
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""

	return parsed.String(), nil
}

func Clip(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)

	if len(runes) <= limit {
		return text
	}

	return string(runes[:limit-1]) + "…"
}
