package privacy_test

import (
	"errors"
	"testing"

	"go.airlance.org/server/internal/domain/privacy"
)

func TestLevelsAllowTheRightPeople(t *testing.T) {
	t.Parallel()

	stranger := privacy.Relation{}
	partner := privacy.Relation{IsContact: true}

	for _, tc := range []struct {
		level                 privacy.Level
		toStranger, toContact bool
	}{
		{privacy.Everyone, true, true},
		{privacy.Contacts, false, true},
		{privacy.Nobody, false, false},
	} {
		if tc.level.Allows(stranger) != tc.toStranger || tc.level.Allows(partner) != tc.toContact {
			t.Errorf("level %d", tc.level)
		}
	}
}

func TestSettingsKeepToWhatEachRowOffers(t *testing.T) {
	t.Parallel()

	if err := privacy.Defaults().Validate(); err != nil {
		t.Fatal(err)
	}

	for name, change := range map[string]func(*privacy.Settings){
		"unknown phone level": func(s *privacy.Settings) { s.Phone = 3 },
		"negative photos":     func(s *privacy.Settings) { s.Photos = -1 },
		"nobody may write":    func(s *privacy.Settings) { s.Messages = privacy.Nobody },
		"search by contacts":  func(s *privacy.Settings) { s.Search = privacy.Contacts },
	} {
		next := privacy.Defaults()
		change(&next)

		if err := next.Validate(); !errors.Is(err, privacy.ErrInvalidSettings) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
