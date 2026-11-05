package contact_test

import (
	"errors"
	"strings"
	"testing"

	"go.airlance.org/server/internal/domain/contact"
)

func TestPhonesBecomeDigits(t *testing.T) {
	t.Parallel()

	got, err := contact.NormalizePhone("+380 (67) 420-48-30")
	if err != nil || got != "380674204830" {
		t.Fatalf("got %q, %v", got, err)
	}

	for _, raw := range []string{"", "123", strings.Repeat("1", 16), "no digits"} {
		if _, err := contact.NormalizePhone(raw); !errors.Is(err, contact.ErrInvalidPhone) {
			t.Errorf("%q: %v", raw, err)
		}
	}
}

func TestNamesAreCleaned(t *testing.T) {
	t.Parallel()

	if got, err := contact.CleanName("  Анна "); err != nil || got != "Анна" {
		t.Fatalf("got %q, %v", got, err)
	}

	for _, raw := range []string{strings.Repeat("я", 65), "a\nb", "a\xffb"} {
		if _, err := contact.CleanName(raw); !errors.Is(err, contact.ErrInvalidName) {
			t.Errorf("%q: %v", raw, err)
		}
	}
}
