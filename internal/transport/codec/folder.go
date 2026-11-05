package codec

import (
	flatbuffers "github.com/google/flatbuffers/go"

	"go.airlance.org/server/internal/transport/wire"
)

const (
	maxFolders     = 64
	maxFolderPeers = 1024
)

const (
	KindFoldersGet     = Kind(wire.MessageKindFoldersGet)
	KindFoldersPut     = Kind(wire.MessageKindFoldersPut)
	KindFoldersData    = Kind(wire.MessageKindFoldersData)
	KindFoldersChanged = Kind(wire.MessageKindFoldersChanged)
)

type Folder struct {
	ID         string
	Name       string
	Emoji      string
	Color      int32
	Categories int32
	Exclusions int32
	Included   []string
	Excluded   []string
}

type FolderList struct {
	Folders          []Folder
	AllChatsPosition int32
	ShowsTags        bool
	Revision         int64
}

func buildStrings(builder *flatbuffers.Builder, values []string) flatbuffers.UOffsetT {
	offsets := make([]flatbuffers.UOffsetT, len(values))
	for index, value := range values {
		offsets[index] = builder.CreateString(value)
	}

	builder.StartVector(flatbuffers.SizeUOffsetT, len(offsets), flatbuffers.SizeUOffsetT)

	for index := len(offsets) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(offsets[index])
	}

	return builder.EndVector(len(offsets))
}

func buildFolders(builder *flatbuffers.Builder, folders []Folder) flatbuffers.UOffsetT {
	offsets := make([]flatbuffers.UOffsetT, len(folders))

	for index, next := range folders {
		id := builder.CreateString(next.ID)
		name := builder.CreateString(next.Name)
		emoji := builder.CreateString(next.Emoji)
		included := buildStrings(builder, next.Included)
		excluded := buildStrings(builder, next.Excluded)
		wire.FolderDataStart(builder)
		wire.FolderDataAddId(builder, id)
		wire.FolderDataAddName(builder, name)
		wire.FolderDataAddEmoji(builder, emoji)
		wire.FolderDataAddColor(builder, next.Color)
		wire.FolderDataAddCategories(builder, next.Categories)
		wire.FolderDataAddExclusions(builder, next.Exclusions)
		wire.FolderDataAddIncluded(builder, included)
		wire.FolderDataAddExcluded(builder, excluded)
		offsets[index] = wire.FolderDataEnd(builder)
	}

	builder.StartVector(flatbuffers.SizeUOffsetT, len(offsets), flatbuffers.SizeUOffsetT)

	for index := len(offsets) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(offsets[index])
	}

	return builder.EndVector(len(offsets))
}

func readFolder(row *wire.FolderData) Folder {
	if row.IncludedLength() > maxFolderPeers || row.ExcludedLength() > maxFolderPeers {
		panic("too many peers")
	}

	next := Folder{
		ID:         string(row.Id()),
		Name:       string(row.Name()),
		Emoji:      string(row.Emoji()),
		Color:      row.Color(),
		Categories: row.Categories(),
		Exclusions: row.Exclusions(),
		Included:   make([]string, row.IncludedLength()),
		Excluded:   make([]string, row.ExcludedLength()),
	}

	for index := range next.Included {
		next.Included[index] = string(row.Included(index))
	}

	for index := range next.Excluded {
		next.Excluded[index] = string(row.Excluded(index))
	}

	return next
}

func DecodeFoldersPut(body []byte) (FolderList, error) {
	var list FolderList

	err := safely(func() {
		table := wire.GetRootAsFoldersPut(body, 0)
		if table.FoldersLength() > maxFolders {
			panic("too many folders")
		}

		list = FolderList{
			AllChatsPosition: table.AllChatsPosition(),
			ShowsTags:        table.ShowsTags(),
			Folders:          make([]Folder, table.FoldersLength()),
		}

		var row wire.FolderData

		for index := range list.Folders {
			if !table.Folders(&row, index) {
				panic("folder")
			}

			list.Folders[index] = readFolder(&row)
		}
	})

	return list, err
}

func EncodeFoldersPut(list FolderList) []byte {
	builder := flatbuffers.NewBuilder(512)
	folders := buildFolders(builder, list.Folders)
	wire.FoldersPutStart(builder)
	wire.FoldersPutAddFolders(builder, folders)
	wire.FoldersPutAddAllChatsPosition(builder, list.AllChatsPosition)
	wire.FoldersPutAddShowsTags(builder, list.ShowsTags)
	builder.Finish(wire.FoldersPutEnd(builder))

	return builder.FinishedBytes()
}

func EncodeFoldersData(list FolderList) []byte {
	builder := flatbuffers.NewBuilder(512)
	folders := buildFolders(builder, list.Folders)
	wire.FoldersDataStart(builder)
	wire.FoldersDataAddFolders(builder, folders)
	wire.FoldersDataAddAllChatsPosition(builder, list.AllChatsPosition)
	wire.FoldersDataAddShowsTags(builder, list.ShowsTags)
	wire.FoldersDataAddRevision(builder, list.Revision)
	builder.Finish(wire.FoldersDataEnd(builder))

	return builder.FinishedBytes()
}

func DecodeFoldersData(body []byte) (FolderList, error) {
	var list FolderList

	err := safely(func() {
		table := wire.GetRootAsFoldersData(body, 0)
		if table.FoldersLength() > maxFolders {
			panic("too many folders")
		}

		list = FolderList{
			AllChatsPosition: table.AllChatsPosition(),
			ShowsTags:        table.ShowsTags(),
			Revision:         table.Revision(),
			Folders:          make([]Folder, table.FoldersLength()),
		}

		var row wire.FolderData

		for index := range list.Folders {
			if !table.Folders(&row, index) {
				panic("folder")
			}

			list.Folders[index] = readFolder(&row)
		}
	})

	return list, err
}

func EncodeFoldersChanged() []byte {
	builder := flatbuffers.NewBuilder(16)
	wire.FoldersChangedStart(builder)
	builder.Finish(wire.FoldersChangedEnd(builder))

	return builder.FinishedBytes()
}
