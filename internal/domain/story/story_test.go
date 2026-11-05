package story_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"go.airlance.org/server/internal/domain/story"
)

func TestNewKeepsOnlyAllowedStories(t *testing.T) {
	t.Parallel()

	good := story.Draft{ClientID: "c", MediaID: "m", LifetimeHours: 24}
	now := time.Unix(1000, 0)

	got, err := story.New("id", "anna", good, now)
	if err != nil {
		t.Fatal(err)
	}

	if !got.ExpiresAt.Equal(now.Add(24 * time.Hour)) {
		t.Fatalf("expires at %v", got.ExpiresAt)
	}

	for name, tc := range map[string]struct {
		change func(*story.Draft)
		want   error
	}{
		"six hours":      {func(d *story.Draft) { d.LifetimeHours = 6 }, story.ErrInvalidLifetime},
		"no lifetime":    {func(d *story.Draft) { d.LifetimeHours = 0 }, story.ErrInvalidLifetime},
		"no client id":   {func(d *story.Draft) { d.ClientID = "" }, story.ErrInvalidClientID},
		"long client id": {func(d *story.Draft) { d.ClientID = strings.Repeat("x", 65) }, story.ErrInvalidClientID},
		"long meta":      {func(d *story.Draft) { d.Meta = strings.Repeat("x", story.MaxMetaBytes+1) }, story.ErrInvalidMeta},
		"broken meta":    {func(d *story.Draft) { d.Meta = "a\xffb" }, story.ErrInvalidMeta},
		"no media":       {func(d *story.Draft) { d.MediaID = "" }, story.ErrInvalidMedia},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			draft := good
			tc.change(&draft)

			if _, err := story.New("id", "anna", draft, now); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}
