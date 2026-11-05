package codec_test

import (
	"reflect"
	"testing"

	"go.airlance.org/server/internal/transport/codec"
)

func TestFoldersSurviveTheWire(t *testing.T) {
	t.Parallel()

	list := codec.FolderList{
		Folders: []codec.Folder{
			{ID: "b", Name: "Работа", Emoji: "💼", Color: 2, Categories: 3, Exclusions: 1, Included: []string{"c1", "c2"}, Excluded: []string{"c3"}},
			{ID: "a", Name: "Семья", Color: -1, Included: []string{}, Excluded: []string{}},
		},
		AllChatsPosition: 1,
		ShowsTags:        true,
	}

	got, err := codec.DecodeFoldersPut(codec.EncodeFoldersPut(list))
	if err != nil || !reflect.DeepEqual(got, list) {
		t.Fatalf("got %+v, %v", got, err)
	}

	list.Revision = 7

	back, err := codec.DecodeFoldersData(codec.EncodeFoldersData(list))
	if err != nil || !reflect.DeepEqual(back, list) {
		t.Fatalf("got %+v, %v", back, err)
	}
}
