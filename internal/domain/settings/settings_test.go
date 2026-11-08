package settings_test

import (
	"bytes"
	"errors"
	"testing"

	"go.airlance.org/server/internal/domain/settings"
)

func TestKeys(t *testing.T) {
	t.Parallel()

	for _, good := range []string{"appearance", "chat.theme", "a1_b"} {
		if err := settings.CheckKey(good); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}

	for _, bad := range []string{"", "Appearance", "1a", "a b", "a/b", string(bytes.Repeat([]byte("a"), settings.MaxKeyLen+1))} {
		if err := settings.CheckKey(bad); !errors.Is(err, settings.ErrInvalid) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestDataSize(t *testing.T) {
	t.Parallel()

	if err := settings.CheckData(make([]byte, settings.MaxDataSize)); err != nil {
		t.Fatal(err)
	}

	if err := settings.CheckData(make([]byte, settings.MaxDataSize+1)); !errors.Is(err, settings.ErrInvalid) {
		t.Fatalf("too big: %v", err)
	}
}

func TestWallpaperNeedsAnIDAFileAndAName(t *testing.T) {
	t.Parallel()

	if err := settings.CheckWallpaper(settings.Wallpaper{ID: "w", Name: "Ліс", MediaID: "m"}); err != nil {
		t.Fatal(err)
	}

	for _, bad := range []settings.Wallpaper{{Name: "x", MediaID: "m"}, {ID: "w", MediaID: "m"}, {ID: "w", Name: "x"}} {
		if err := settings.CheckWallpaper(bad); !errors.Is(err, settings.ErrInvalid) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}
