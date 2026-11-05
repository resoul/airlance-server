package story

import (
	"errors"
	"time"
	"unicode/utf8"
)

const (
	MaxMetaBytes        = 64 << 10
	MaxPhotoBytes int64 = 20 << 20
	MaxVideoBytes int64 = 200 << 20
	MaxCoverBytes int64 = 10 << 20
	MaxFeed             = 500
	maxClientID         = 64
)

var (
	ErrInvalidLifetime = errors.New("invalid story lifetime")
	ErrInvalidClientID = errors.New("invalid story client id")
	ErrInvalidMeta     = errors.New("invalid story meta")
	ErrInvalidMedia    = errors.New("invalid story media")
	ErrNotFound        = errors.New("story not found")
)

type Owner struct {
	Subject      string
	FirstName    string
	LastName     string
	PhotoMediaID string
	VideoMediaID string
}

type Story struct {
	ID           string
	ClientID     string
	Owner        Owner
	MediaID      string
	CoverMediaID string
	IsVideo      bool
	Meta         string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	Seen         bool
	ViewCount    int
}

type Draft struct {
	ClientID      string
	MediaID       string
	CoverMediaID  string
	IsVideo       bool
	LifetimeHours int
	Meta          string
}

func New(id, owner string, draft Draft, now time.Time) (Story, error) {
	switch draft.LifetimeHours {
	case 12, 24, 48:
	default:
		return Story{}, ErrInvalidLifetime
	}

	if draft.ClientID == "" || len(draft.ClientID) > maxClientID {
		return Story{}, ErrInvalidClientID
	}

	if len(draft.Meta) > MaxMetaBytes || !utf8.ValidString(draft.Meta) {
		return Story{}, ErrInvalidMeta
	}

	if draft.MediaID == "" {
		return Story{}, ErrInvalidMedia
	}

	created := now.UTC()

	return Story{
		ID:           id,
		ClientID:     draft.ClientID,
		Owner:        Owner{Subject: owner},
		MediaID:      draft.MediaID,
		CoverMediaID: draft.CoverMediaID,
		IsVideo:      draft.IsVideo,
		Meta:         draft.Meta,
		CreatedAt:    created,
		ExpiresAt:    created.Add(time.Duration(draft.LifetimeHours) * time.Hour),
	}, nil
}
