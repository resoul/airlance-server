package directory_test

import (
	"errors"
	"strings"
	"testing"

	"go.airlance.org/server/internal/domain/directory"
)

func TestQueriesAreCleanedAndBounded(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]string{
		"anna":     "anna",
		"  anna  ": "anna",
		"@anna":    "anna",
		" @ anna ": "anna",
		"Шев":      "Шев",
		"Анна Ш":   "Анна Ш",
		"abc":      "abc",
	} {
		got, err := directory.NormalizeQuery(raw)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v", raw, got, err)
		}
	}

	for _, raw := range []string{"", "  ", "ab", "@ab", "Ан", strings.Repeat("a", 65), "a\xffb\xfe"[:2]} {
		if _, err := directory.NormalizeQuery(raw); !errors.Is(err, directory.ErrInvalidQuery) {
			t.Errorf("%q: %v", raw, err)
		}
	}

	if _, err := directory.NormalizeQuery("abc\xff"); !errors.Is(err, directory.ErrInvalidQuery) {
		t.Errorf("broken text: %v", err)
	}
}
