//go:build integration

package postgres_test

import (
	"testing"
	"time"

	domain "go.airlance.org/server/internal/domain/settings"
	"go.airlance.org/server/internal/domain/update"
	"go.airlance.org/server/internal/repository/postgres"
)

func TestSettingsKeepOneRecordPerPersonAndKeyWithGrowingVersions(t *testing.T) {
	s := newStage(t)
	repository := postgres.NewSettings(s.pool)

	empty, err := repository.Get(s.ctx, "anna", "appearance")
	if err != nil || empty.Version != 0 || len(empty.Data) != 0 {
		t.Fatalf("got %+v, %v", empty, err)
	}

	first, err := repository.Put(s.ctx, "anna", "appearance", []byte(`{"mode":"dark"}`), time.Now())
	if err != nil || first.Version != 1 {
		t.Fatalf("got %+v, %v", first, err)
	}

	second, err := repository.Put(s.ctx, "anna", "appearance", []byte(`{"mode":"light"}`), time.Now())
	if err != nil || second.Version != 2 || string(second.Data) != `{"mode":"light"}` {
		t.Fatalf("got %+v, %v", second, err)
	}

	other, err := repository.Get(s.ctx, "petro", "appearance")
	if err != nil || other.Version != 0 {
		t.Fatalf("another person sees it: %+v, %v", other, err)
	}

	journal, err := postgres.NewUpdates(s.pool).After(s.ctx, "anna", 0, 100)
	if err != nil {
		t.Fatal(err)
	}

	tail := journal[len(journal)-1]
	if tail.Kind != update.KindSettings || tail.Setting.Version != 2 || string(tail.Setting.Data) != `{"mode":"light"}` {
		t.Fatalf("my other devices were not told: %+v", tail)
	}
}

func TestWallpapersAreListedInOrderAndTheirFilesAreOpenToEveryone(t *testing.T) {
	s := newStage(t)
	repository := postgres.NewSettings(s.pool)
	media := postgres.NewMedia(s.pool)

	for _, next := range []domain.Wallpaper{
		{ID: "b", Name: "Море", MediaID: "media-b", Sort: 2},
		{ID: "a", Name: "Ліс", MediaID: "media-a", Sort: 1},
	} {
		if err := repository.AddWallpaper(s.ctx, next, time.Now()); err != nil {
			t.Fatal(err)
		}
	}

	list, err := repository.Wallpapers(s.ctx)
	if err != nil || len(list) != 2 || list[0].ID != "a" || list[1].ID != "b" {
		t.Fatalf("got %+v, %v", list, err)
	}

	open, err := media.IsCatalogMedia(s.ctx, "media-a")
	if err != nil || !open {
		t.Fatalf("a catalog file is closed: %v, %v", open, err)
	}

	if open, _ := media.IsCatalogMedia(s.ctx, "somebody-elses"); open {
		t.Fatal("any file is open")
	}

	mediaID, err := repository.RemoveWallpaper(s.ctx, "a")
	if err != nil || mediaID != "media-a" {
		t.Fatalf("got %q, %v", mediaID, err)
	}

	if _, err := repository.RemoveWallpaper(s.ctx, "a"); err == nil {
		t.Fatal("removed twice")
	}
}
