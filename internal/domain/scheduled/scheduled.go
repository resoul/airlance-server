package scheduled

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"go.airlance.org/server/internal/domain/message"
)

type Repeat int16

const (
	RepeatNever    Repeat = 0
	RepeatDaily    Repeat = 1
	RepeatWeekly   Repeat = 2
	RepeatBiweekly Repeat = 3
	RepeatMonthly  Repeat = 4
)

const (
	MaxPerChat = 100
	MaxAhead   = 366 * 24 * time.Hour
	MaxPast    = time.Minute
)

var (
	ErrInvalidTime   = errors.New("invalid scheduled time")
	ErrInvalidRepeat = errors.New("invalid repeat rule")
	ErrInvalidZone   = errors.New("invalid time zone")
	ErrTooMany       = errors.New("too many scheduled messages")
	ErrNotFound      = errors.New("scheduled message not found")
)

type Item struct {
	ID       string
	ChatID   string
	Sender   string
	ClientID string
	Text     string
	SendAt   time.Time
	Repeat   Repeat
	Timezone string
	Failure  string
}

func New(id, chatID, sender, clientID, text string, sendAt time.Time, repeat Repeat, zone string, now time.Time) (Item, error) {
	clean, err := CheckText(text)
	if err != nil {
		return Item{}, err
	}

	if clientID == "" || len(clientID) > message.MaxClientIDLength || !utf8.ValidString(clientID) {
		return Item{}, message.ErrInvalidClientID
	}

	if err := CheckTime(sendAt, now); err != nil {
		return Item{}, err
	}

	if repeat < RepeatNever || repeat > RepeatMonthly {
		return Item{}, ErrInvalidRepeat
	}

	if zone == "" {
		zone = time.UTC.String()
	}

	if _, err := time.LoadLocation(zone); err != nil {
		return Item{}, fmt.Errorf("%w: %s", ErrInvalidZone, zone)
	}

	return Item{
		ID: id, ChatID: chatID, Sender: sender, ClientID: clientID, Text: clean,
		SendAt: sendAt, Repeat: repeat, Timezone: zone,
	}, nil
}

func CheckText(text string) (string, error) {
	clean := strings.TrimSpace(text)
	if clean == "" || !utf8.ValidString(clean) || utf8.RuneCountInString(clean) > message.MaxTextLength {
		return "", message.ErrInvalidText
	}

	return clean, nil
}

func CheckTime(sendAt, now time.Time) error {
	if sendAt.Before(now.Add(-MaxPast)) || sendAt.After(now.Add(MaxAhead)) {
		return ErrInvalidTime
	}

	return nil
}

func (i Item) DeliveryClientID() string {
	if i.Repeat == RepeatNever {
		return i.ClientID
	}

	return fmt.Sprintf("%s:%d", i.ClientID, i.SendAt.Unix())
}

func (i Item) Next(now time.Time) (time.Time, bool) {
	if i.Repeat == RepeatNever {
		return time.Time{}, false
	}

	zone, err := time.LoadLocation(i.Timezone)
	if err != nil {
		zone = time.UTC
	}

	next := i.SendAt.In(zone)
	for !next.After(now) {
		switch i.Repeat {
		case RepeatNever:
			return time.Time{}, false
		case RepeatDaily:
			next = next.AddDate(0, 0, 1)
		case RepeatWeekly:
			next = next.AddDate(0, 0, 7)
		case RepeatBiweekly:
			next = next.AddDate(0, 0, 14)
		case RepeatMonthly:
			next = next.AddDate(0, 1, 0)
		}
	}

	return next, true
}

func FailureOf(err error) string {
	switch {
	case errors.Is(err, message.ErrBlocked):
		return "blocked"
	case errors.Is(err, message.ErrNotAllowed):
		return "not_allowed"
	case errors.Is(err, message.ErrChatNotFound):
		return "no_access"
	default:
		return ""
	}
}
