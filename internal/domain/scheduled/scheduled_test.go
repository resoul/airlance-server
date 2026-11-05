package scheduled_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/scheduled"
)

var now = time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC)

func TestAnItemIsChecked(t *testing.T) {
	t.Parallel()

	make := func(text, client string, at time.Time, repeat scheduled.Repeat, zone string) error {
		_, err := scheduled.New("i", "c", "anna", client, text, at, repeat, zone, now)

		return err
	}
	soon := now.Add(time.Hour)

	cases := []struct {
		err  error
		want error
	}{
		{make(" ", "l", soon, 0, ""), message.ErrInvalidText},
		{make(strings.Repeat("я", message.MaxTextLength+1), "l", soon, 0, ""), message.ErrInvalidText},
		{make("hi", "", soon, 0, ""), message.ErrInvalidClientID},
		{make("hi", "l", now.Add(-time.Hour), 0, ""), scheduled.ErrInvalidTime},
		{make("hi", "l", now.Add(400*24*time.Hour), 0, ""), scheduled.ErrInvalidTime},
		{make("hi", "l", soon, 9, ""), scheduled.ErrInvalidRepeat},
		{make("hi", "l", soon, 1, "Mars/Olympus"), scheduled.ErrInvalidZone},
	}

	for index, next := range cases {
		if !errors.Is(next.err, next.want) {
			t.Errorf("%d: got %v", index, next.err)
		}
	}

	item, err := scheduled.New("i", "c", "anna", "l", "  hi ", soon, 0, "", now)
	if err != nil || item.Text != "hi" || item.Timezone != "UTC" {
		t.Fatalf("got %+v, %v", item, err)
	}
}

func TestARepeatKeepsTheLocalTimeAcrossClockChanges(t *testing.T) {
	t.Parallel()

	zone, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Fatal(err)
	}

	before := time.Date(2026, 3, 28, 9, 0, 0, 0, zone)
	item := scheduled.Item{SendAt: before, Repeat: scheduled.RepeatDaily, Timezone: "Europe/Kyiv"}

	next, ok := item.Next(before)
	if !ok || next.In(zone).Hour() != 9 || next.Sub(before) != 23*time.Hour {
		t.Fatalf("got %v, %v after %v", next, ok, next.Sub(before))
	}
}

func TestARepeatSkipsWhatItMissed(t *testing.T) {
	t.Parallel()

	item := scheduled.Item{SendAt: now.Add(-71 * time.Hour), Repeat: scheduled.RepeatDaily, Timezone: "UTC"}

	next, ok := item.Next(now)
	if !ok || !next.Equal(now.Add(time.Hour)) {
		t.Fatalf("got %v, %v", next, ok)
	}

	once := scheduled.Item{SendAt: now}
	if _, ok := once.Next(now); ok {
		t.Fatal("a one-time message repeated")
	}
}

func TestEachOccurrenceHasItsOwnClientID(t *testing.T) {
	t.Parallel()

	once := scheduled.Item{ClientID: "l", SendAt: now}
	if once.DeliveryClientID() != "l" {
		t.Fatal(once.DeliveryClientID())
	}

	first := scheduled.Item{ClientID: "l", SendAt: now, Repeat: scheduled.RepeatDaily}
	second := first
	second.SendAt = now.Add(24 * time.Hour)

	if first.DeliveryClientID() == second.DeliveryClientID() || first.DeliveryClientID() == "l" {
		t.Fatal("occurrences share an id")
	}
}
