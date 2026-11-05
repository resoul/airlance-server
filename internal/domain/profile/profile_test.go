package profile_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"go.airlance.org/server/internal/domain/profile"
)

func TestNewAppliesTheRules(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		fields profile.Fields
		want   error
	}{
		{"a name alone", profile.Fields{FirstName: "Анна"}, nil},
		{"everything", profile.Fields{FirstName: "Анна", LastName: "Ш", Username: "anna_s1", Bio: "Привет"}, nil},
		{"no name", profile.Fields{FirstName: "   "}, profile.ErrInvalidName},
		{"a long name", profile.Fields{FirstName: strings.Repeat("я", 65)}, profile.ErrInvalidName},
		{"a control character", profile.Fields{FirstName: "a\x00b"}, profile.ErrInvalidName},
		{"broken text", profile.Fields{FirstName: "a\xffb"}, profile.ErrInvalidName},
		{"a short username", profile.Fields{FirstName: "A", Username: "abcd"}, profile.ErrInvalidUsername},
		{"a username from a digit", profile.Fields{FirstName: "A", Username: "1abcde"}, profile.ErrInvalidUsername},
		{"a username ending in _", profile.Fields{FirstName: "A", Username: "abcde_"}, profile.ErrInvalidUsername},
		{"a cyrillic username", profile.Fields{FirstName: "A", Username: "анна_шева"}, profile.ErrInvalidUsername},
		{"a long bio", profile.Fields{FirstName: "A", Bio: strings.Repeat("я", 71)}, profile.ErrInvalidBio},
		{"a long phone", profile.Fields{FirstName: "A", Phone: strings.Repeat("1", 33)}, profile.ErrInvalidPhone},
		{"a phone with a line break", profile.Fields{FirstName: "A", Phone: "1\n2"}, profile.ErrInvalidPhone},
		{"a video without a photo", profile.Fields{FirstName: "A", VideoMediaID: "v"}, profile.ErrInvalidMedia},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := profile.New("user", tc.fields, time.Unix(1, 0))
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestNewTrimsWhatItKeeps(t *testing.T) {
	t.Parallel()

	got, err := profile.New("user", profile.Fields{FirstName: "  Анна ", Bio: " x "}, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}

	if got.Fields.FirstName != "Анна" || got.Fields.Bio != "x" {
		t.Fatalf("not trimmed: %+v", got.Fields)
	}
}
