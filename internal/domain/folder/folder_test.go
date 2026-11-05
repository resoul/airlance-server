package folder_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"go.airlance.org/server/internal/domain/folder"
)

func valid() folder.Folder {
	return folder.Folder{ID: "f1", Name: " Работа ", Color: 2, Categories: 3, Exclusions: 1, Included: []string{"c1"}}
}

func TestAListIsChecked(t *testing.T) {
	t.Parallel()

	list, err := folder.List{Folders: []folder.Folder{valid()}, AllChatsPosition: 1}.Checked()
	if err != nil || list.Folders[0].Name != "Работа" {
		t.Fatalf("got %+v, %v", list, err)
	}

	cases := map[string]func(*folder.Folder){
		"no id":      func(f *folder.Folder) { f.ID = "" },
		"no name":    func(f *folder.Folder) { f.Name = "  " },
		"long name":  func(f *folder.Folder) { f.Name = strings.Repeat("я", 25) },
		"long emoji": func(f *folder.Folder) { f.Emoji = strings.Repeat("😀", 17) },
		"a color":    func(f *folder.Folder) { f.Color = 7 },
		"categories": func(f *folder.Folder) { f.Categories = 32 },
		"exclusions": func(f *folder.Folder) { f.Exclusions = 4 },
		"a peer":     func(f *folder.Folder) { f.Excluded = []string{""} },
		"many peers": func(f *folder.Folder) { f.Included = make([]string, folder.MaxPeers+1) },
	}

	for name, change := range cases {
		next := valid()
		change(&next)

		if _, err := (folder.List{Folders: []folder.Folder{next}}).Checked(); !errors.Is(err, folder.ErrInvalid) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestAListHoldsItsShape(t *testing.T) {
	t.Parallel()

	if _, err := (folder.List{AllChatsPosition: 1}).Checked(); !errors.Is(err, folder.ErrInvalid) {
		t.Fatalf("a position past the end: %v", err)
	}

	if _, err := (folder.List{Folders: []folder.Folder{valid(), valid()}}).Checked(); !errors.Is(err, folder.ErrInvalid) {
		t.Fatalf("a repeated id: %v", err)
	}

	many := make([]folder.Folder, folder.MaxFolders+1)
	for index := range many {
		many[index] = valid()
		many[index].ID = fmt.Sprintf("f%d", index)
	}

	if _, err := (folder.List{Folders: many}).Checked(); !errors.Is(err, folder.ErrInvalid) {
		t.Fatalf("too many: %v", err)
	}

	if !folder.Empty().ShowsTags {
		t.Fatal("tags are on by default")
	}
}
