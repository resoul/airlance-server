package codec

import (
	flatbuffers "github.com/google/flatbuffers/go"

	"go.airlance.org/server/internal/transport/wire"
)

const (
	KindMessageEdit     = Kind(wire.MessageKindMessageEdit)
	KindMessageDelete   = Kind(wire.MessageKindMessageDelete)
	KindMessagesDeleted = Kind(wire.MessageKindMessagesDeleted)

	maxEntities  = 256
	maxDeleteIDs = 100
)

type Entity struct {
	Kind   int32
	Offset int32
	Length int32
	UserID string
}

type MessageEdit struct {
	ChatID    string
	MessageID int64
	Text      string
	Entities  []Entity
}

type MessageDelete struct {
	ChatID      string
	MessageIDs  []int64
	ForEveryone bool
}

func buildEntities(
	builder *flatbuffers.Builder,
	entities []Entity,
	start func(*flatbuffers.Builder, int) flatbuffers.UOffsetT,
) flatbuffers.UOffsetT {
	offsets := make([]flatbuffers.UOffsetT, len(entities))

	for index, next := range entities {
		user := builder.CreateString(next.UserID)
		wire.MessageEntityStart(builder)
		wire.MessageEntityAddKind(builder, next.Kind)
		wire.MessageEntityAddOffset(builder, next.Offset)
		wire.MessageEntityAddLength(builder, next.Length)
		wire.MessageEntityAddUserId(builder, user)
		offsets[index] = wire.MessageEntityEnd(builder)
	}

	start(builder, len(offsets))

	for index := len(offsets) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(offsets[index])
	}

	return builder.EndVector(len(offsets))
}

func readEntities(count int, at func(*wire.MessageEntity, int) bool) []Entity {
	if count > maxEntities {
		panic("too many entities")
	}

	if count == 0 {
		return nil
	}

	entities := make([]Entity, count)

	var row wire.MessageEntity

	for index := range entities {
		if !at(&row, index) {
			panic("entity")
		}

		entities[index] = Entity{
			Kind: row.Kind(), Offset: row.Offset(), Length: row.Length(), UserID: string(row.UserId()),
		}
	}

	return entities
}

func buildIDs(
	builder *flatbuffers.Builder,
	ids []int64,
	start func(*flatbuffers.Builder, int) flatbuffers.UOffsetT,
) flatbuffers.UOffsetT {
	start(builder, len(ids))

	for index := len(ids) - 1; index >= 0; index-- {
		builder.PrependInt64(ids[index])
	}

	return builder.EndVector(len(ids))
}

func readIDs(count int, at func(int) int64) []int64 {
	if count > maxDeleteIDs {
		panic("too many ids")
	}

	ids := make([]int64, count)
	for index := range ids {
		ids[index] = at(index)
	}

	return ids
}

func DecodeMessageEdit(body []byte) (MessageEdit, error) {
	var edit MessageEdit

	err := safely(func() {
		table := wire.GetRootAsMessageEdit(body, 0)
		edit = MessageEdit{
			ChatID:    string(table.ChatId()),
			MessageID: table.MessageId(),
			Text:      string(table.Text()),
			Entities:  readEntities(table.EntitiesLength(), table.Entities),
		}
	})

	return edit, err
}

func EncodeMessageEdit(edit MessageEdit) []byte {
	builder := flatbuffers.NewBuilder(len(edit.Text) + 128)
	chat := builder.CreateString(edit.ChatID)
	text := builder.CreateString(edit.Text)
	entities := buildEntities(builder, edit.Entities, wire.MessageEditStartEntitiesVector)
	wire.MessageEditStart(builder)
	wire.MessageEditAddChatId(builder, chat)
	wire.MessageEditAddMessageId(builder, edit.MessageID)
	wire.MessageEditAddText(builder, text)
	wire.MessageEditAddEntities(builder, entities)
	builder.Finish(wire.MessageEditEnd(builder))

	return builder.FinishedBytes()
}

func DecodeMessageDelete(body []byte) (MessageDelete, error) {
	var remove MessageDelete

	err := safely(func() {
		table := wire.GetRootAsMessageDelete(body, 0)
		remove = MessageDelete{
			ChatID:      string(table.ChatId()),
			MessageIDs:  readIDs(table.MessageIdsLength(), table.MessageIds),
			ForEveryone: table.ForEveryone(),
		}
	})

	return remove, err
}

func EncodeMessageDelete(remove MessageDelete) []byte {
	builder := flatbuffers.NewBuilder(128)
	chat := builder.CreateString(remove.ChatID)
	ids := buildIDs(builder, remove.MessageIDs, wire.MessageDeleteStartMessageIdsVector)
	wire.MessageDeleteStart(builder)
	wire.MessageDeleteAddChatId(builder, chat)
	wire.MessageDeleteAddMessageIds(builder, ids)
	wire.MessageDeleteAddForEveryone(builder, remove.ForEveryone)
	builder.Finish(wire.MessageDeleteEnd(builder))

	return builder.FinishedBytes()
}

func EncodeMessagesDeleted(chatID string, ids []int64) []byte {
	builder := flatbuffers.NewBuilder(128)
	chat := builder.CreateString(chatID)
	vector := buildIDs(builder, ids, wire.MessagesDeletedStartMessageIdsVector)
	wire.MessagesDeletedStart(builder)
	wire.MessagesDeletedAddChatId(builder, chat)
	wire.MessagesDeletedAddMessageIds(builder, vector)
	builder.Finish(wire.MessagesDeletedEnd(builder))

	return builder.FinishedBytes()
}

func DecodeMessagesDeleted(body []byte) (string, []int64, error) {
	var (
		chatID string
		ids    []int64
	)

	err := safely(func() {
		table := wire.GetRootAsMessagesDeleted(body, 0)
		chatID = string(table.ChatId())
		ids = readIDs(table.MessageIdsLength(), table.MessageIds)
	})

	return chatID, ids, err
}
