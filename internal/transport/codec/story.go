package codec

import (
	flatbuffers "github.com/google/flatbuffers/go"

	"go.airlance.org/server/internal/transport/wire"
)

const maxStories = 1024

const (
	KindStoryPublish   = Kind(wire.MessageKindStoryPublish)
	KindStoryData      = Kind(wire.MessageKindStoryData)
	KindStoriesGet     = Kind(wire.MessageKindStoriesGet)
	KindStoriesData    = Kind(wire.MessageKindStoriesData)
	KindStoryView      = Kind(wire.MessageKindStoryView)
	KindStoryDelete    = Kind(wire.MessageKindStoryDelete)
	KindStoriesChanged = Kind(wire.MessageKindStoriesChanged)
)

type StoryPublish struct {
	ClientID      string
	MediaID       string
	CoverMediaID  string
	IsVideo       bool
	LifetimeHours int32
	Meta          string
}

type StoryData struct {
	StoryID           string
	ClientID          string
	OwnerSubject      string
	OwnerFirstName    string
	OwnerLastName     string
	OwnerPhotoMediaID string
	OwnerVideoMediaID string
	MediaID           string
	CoverMediaID      string
	IsVideo           bool
	Meta              string
	CreatedAtUnix     int64
	ExpiresAtUnix     int64
	Seen              bool
	ViewCount         int32
}

func DecodeStoryPublish(body []byte) (StoryPublish, error) {
	var next StoryPublish

	err := safely(func() {
		table := wire.GetRootAsStoryPublish(body, 0)
		next = StoryPublish{
			ClientID:      string(table.ClientId()),
			MediaID:       string(table.MediaId()),
			CoverMediaID:  string(table.CoverMediaId()),
			IsVideo:       table.IsVideo(),
			LifetimeHours: table.LifetimeHours(),
			Meta:          string(table.Meta()),
		}
	})

	return next, err
}

func EncodeStoryPublish(next StoryPublish) []byte {
	builder := flatbuffers.NewBuilder(256 + len(next.Meta))
	client := builder.CreateString(next.ClientID)
	media := builder.CreateString(next.MediaID)
	cover := builder.CreateString(next.CoverMediaID)
	meta := builder.CreateString(next.Meta)
	wire.StoryPublishStart(builder)
	wire.StoryPublishAddClientId(builder, client)
	wire.StoryPublishAddMediaId(builder, media)
	wire.StoryPublishAddCoverMediaId(builder, cover)
	wire.StoryPublishAddIsVideo(builder, next.IsVideo)
	wire.StoryPublishAddLifetimeHours(builder, next.LifetimeHours)
	wire.StoryPublishAddMeta(builder, meta)
	builder.Finish(wire.StoryPublishEnd(builder))

	return builder.FinishedBytes()
}

func buildStory(builder *flatbuffers.Builder, next StoryData) flatbuffers.UOffsetT {
	id := builder.CreateString(next.StoryID)
	client := builder.CreateString(next.ClientID)
	owner := builder.CreateString(next.OwnerSubject)
	first := builder.CreateString(next.OwnerFirstName)
	last := builder.CreateString(next.OwnerLastName)
	photo := builder.CreateString(next.OwnerPhotoMediaID)
	video := builder.CreateString(next.OwnerVideoMediaID)
	media := builder.CreateString(next.MediaID)
	cover := builder.CreateString(next.CoverMediaID)
	meta := builder.CreateString(next.Meta)
	wire.StoryDataStart(builder)
	wire.StoryDataAddStoryId(builder, id)
	wire.StoryDataAddClientId(builder, client)
	wire.StoryDataAddOwnerSubject(builder, owner)
	wire.StoryDataAddOwnerFirstName(builder, first)
	wire.StoryDataAddOwnerLastName(builder, last)
	wire.StoryDataAddOwnerPhotoMediaId(builder, photo)
	wire.StoryDataAddOwnerVideoMediaId(builder, video)
	wire.StoryDataAddMediaId(builder, media)
	wire.StoryDataAddCoverMediaId(builder, cover)
	wire.StoryDataAddIsVideo(builder, next.IsVideo)
	wire.StoryDataAddMeta(builder, meta)
	wire.StoryDataAddCreatedAtUnix(builder, next.CreatedAtUnix)
	wire.StoryDataAddExpiresAtUnix(builder, next.ExpiresAtUnix)
	wire.StoryDataAddSeen(builder, next.Seen)
	wire.StoryDataAddViewCount(builder, next.ViewCount)

	return wire.StoryDataEnd(builder)
}

func readStory(table *wire.StoryData) StoryData {
	return StoryData{
		StoryID:           string(table.StoryId()),
		ClientID:          string(table.ClientId()),
		OwnerSubject:      string(table.OwnerSubject()),
		OwnerFirstName:    string(table.OwnerFirstName()),
		OwnerLastName:     string(table.OwnerLastName()),
		OwnerPhotoMediaID: string(table.OwnerPhotoMediaId()),
		OwnerVideoMediaID: string(table.OwnerVideoMediaId()),
		MediaID:           string(table.MediaId()),
		CoverMediaID:      string(table.CoverMediaId()),
		IsVideo:           table.IsVideo(),
		Meta:              string(table.Meta()),
		CreatedAtUnix:     table.CreatedAtUnix(),
		ExpiresAtUnix:     table.ExpiresAtUnix(),
		Seen:              table.Seen(),
		ViewCount:         table.ViewCount(),
	}
}

func EncodeStoryData(next StoryData) []byte {
	builder := flatbuffers.NewBuilder(512 + len(next.Meta))
	builder.Finish(buildStory(builder, next))

	return builder.FinishedBytes()
}

func DecodeStoryData(body []byte) (StoryData, error) {
	var next StoryData

	err := safely(func() {
		next = readStory(wire.GetRootAsStoryData(body, 0))
	})

	return next, err
}

func EncodeStoriesData(stories []StoryData) []byte {
	builder := flatbuffers.NewBuilder(1024)
	offsets := make([]flatbuffers.UOffsetT, len(stories))

	for index, next := range stories {
		offsets[index] = buildStory(builder, next)
	}

	wire.StoriesDataStartStoriesVector(builder, len(offsets))

	for index := len(offsets) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(offsets[index])
	}

	vector := builder.EndVector(len(offsets))
	wire.StoriesDataStart(builder)
	wire.StoriesDataAddStories(builder, vector)
	builder.Finish(wire.StoriesDataEnd(builder))

	return builder.FinishedBytes()
}

func DecodeStoriesData(body []byte) ([]StoryData, error) {
	var stories []StoryData

	err := safely(func() {
		table := wire.GetRootAsStoriesData(body, 0)
		if table.StoriesLength() > maxStories {
			panic("too many stories")
		}

		var row wire.StoryData

		for index := range table.StoriesLength() {
			if !table.Stories(&row, index) {
				panic("story")
			}

			stories = append(stories, readStory(&row))
		}
	})

	return stories, err
}

func EncodeStoryView(id string) []byte {
	builder := flatbuffers.NewBuilder(64)
	text := builder.CreateString(id)
	wire.StoryViewStart(builder)
	wire.StoryViewAddStoryId(builder, text)
	builder.Finish(wire.StoryViewEnd(builder))

	return builder.FinishedBytes()
}

func DecodeStoryView(body []byte) (string, error) {
	var id string

	err := safely(func() {
		id = string(wire.GetRootAsStoryView(body, 0).StoryId())
	})

	return id, err
}

func EncodeStoryDelete(id string) []byte {
	builder := flatbuffers.NewBuilder(64)
	text := builder.CreateString(id)
	wire.StoryDeleteStart(builder)
	wire.StoryDeleteAddStoryId(builder, text)
	builder.Finish(wire.StoryDeleteEnd(builder))

	return builder.FinishedBytes()
}

func DecodeStoryDelete(body []byte) (string, error) {
	var id string

	err := safely(func() {
		id = string(wire.GetRootAsStoryDelete(body, 0).StoryId())
	})

	return id, err
}

func EncodeStoriesChanged() []byte {
	builder := flatbuffers.NewBuilder(16)
	wire.StoriesChangedStart(builder)
	builder.Finish(wire.StoriesChangedEnd(builder))

	return builder.FinishedBytes()
}
