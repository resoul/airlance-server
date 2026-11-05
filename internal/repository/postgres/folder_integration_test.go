//go:build integration

package postgres_test

import (
	"testing"
	"time"

	domain "go.airlance.org/server/internal/domain/folder"
	"go.airlance.org/server/internal/domain/update"
	"go.airlance.org/server/internal/repository/postgres"
)

func TestAFolderListKeepsItsOrderAndCountsRevisions(t *testing.T) {
	ctx, pool := openMessages(t)

	if _, err := pool.Exec(ctx, "TRUNCATE chat_folder_lists"); err != nil {
		t.Fatal(err)
	}

	folders := postgres.NewFolders(pool)

	empty, err := folders.Get(ctx, "anna")
	if err != nil || empty.Revision != 0 || !empty.ShowsTags || len(empty.Folders) != 0 {
		t.Fatalf("a new account: %+v, %v", empty, err)
	}

	list := domain.List{
		Folders: []domain.Folder{
			{ID: "b", Name: "Работа", Emoji: "💼", Color: 1, Categories: 3, Included: []string{"c1"}, Excluded: []string{"c2"}},
			{ID: "a", Name: "Семья", Color: domain.NoColor, Exclusions: 2},
		},
		AllChatsPosition: 1,
	}

	saved, err := folders.Put(ctx, "anna", list, time.Now())
	if err != nil || saved.Revision != 1 {
		t.Fatalf("got %+v, %v", saved, err)
	}

	got, err := folders.Get(ctx, "anna")
	if err != nil || got.Folders[0].ID != "b" || got.Folders[1].ID != "a" || got.AllChatsPosition != 1 || got.ShowsTags {
		t.Fatalf("got %+v, %v", got, err)
	}

	first := got.Folders[0]
	if first.Emoji != "💼" || first.Color != 1 || first.Categories != 3 || first.Included[0] != "c1" || first.Excluded[0] != "c2" {
		t.Fatalf("got %+v", first)
	}

	if got.Folders[1].Color != domain.NoColor {
		t.Fatalf("no color was lost: %+v", got.Folders[1])
	}

	cleared, err := folders.Put(ctx, "anna", domain.List{ShowsTags: true}, time.Now())
	if err != nil || cleared.Revision != 2 {
		t.Fatalf("got %+v, %v", cleared, err)
	}

	journal, err := postgres.NewUpdates(pool).After(ctx, "anna", 0, 10)
	if err != nil || len(journal) != 2 || journal[1].Kind != update.KindFolders {
		t.Fatalf("got %+v, %v", journal, err)
	}

	other, err := folders.Get(ctx, "petro")
	if err != nil || other.Revision != 0 {
		t.Fatalf("someone else: %+v, %v", other, err)
	}
}
