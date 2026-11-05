package message_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"go.airlance.org/server/internal/domain/message"
)

func TestTextIsTrimmedAndBounded(t *testing.T) {
	t.Parallel()

	now := time.Unix(100, 0)

	got, err := message.New("c", 1, "a", "local", "  привет \n", now)
	if err != nil || got.Text != "привет" {
		t.Fatalf("got %+v, %v", got, err)
	}

	if _, err := message.New("c", 1, "a", "local", strings.Repeat("я", message.MaxTextLength), now); err != nil {
		t.Fatalf("the longest text: %v", err)
	}

	for name, text := range map[string]string{
		"empty":    "",
		"spaces":   " \n\t",
		"too long": strings.Repeat("я", message.MaxTextLength+1),
		"broken":   "ab\xff",
	} {
		if _, err := message.New("c", 1, "a", "local", text, now); !errors.Is(err, message.ErrInvalidText) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAClientIDIsRequiredAndBounded(t *testing.T) {
	t.Parallel()

	now := time.Unix(100, 0)

	for _, id := range []string{"", strings.Repeat("x", message.MaxClientIDLength+1), "a\xff"} {
		if _, err := message.New("c", 1, "a", id, "hi", now); !errors.Is(err, message.ErrInvalidClientID) {
			t.Errorf("%q: %v", id, err)
		}
	}
}

func TestADirectKeyDoesNotDependOnOrderAndCannotCollideByJoining(t *testing.T) {
	t.Parallel()

	if message.DirectKey("anna", "petro") != message.DirectKey("petro", "anna") {
		t.Fatal("the order changed the key")
	}

	if message.DirectKey("ab", "c") == message.DirectKey("a", "bc") {
		t.Fatal("different people gave one key")
	}
}
