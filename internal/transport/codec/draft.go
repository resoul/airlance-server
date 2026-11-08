package codec

import (
	flatbuffers "github.com/google/flatbuffers/go"

	"go.airlance.org/server/internal/transport/wire"
)

const (
	KindChatArchiveSet = Kind(wire.MessageKindChatArchiveSet)
	KindDraftSet       = Kind(wire.MessageKindDraftSet)
	KindDraftData      = Kind(wire.MessageKindDraftData)
)

type DraftData struct {
	ChatID      string
	Text        string
	UpdatedAtMs int64
}

func DecodeChatArchiveSet(body []byte) (string, bool, error) {
	var (
		chatID     string
		isArchived bool
	)

	err := safely(func() {
		table := wire.GetRootAsChatArchiveSet(body, 0)
		chatID, isArchived = string(table.ChatId()), table.IsArchived()
	})

	return chatID, isArchived, err
}

func EncodeChatArchiveSet(chatID string, isArchived bool) []byte {
	builder := flatbuffers.NewBuilder(64)
	chat := builder.CreateString(chatID)
	wire.ChatArchiveSetStart(builder)
	wire.ChatArchiveSetAddChatId(builder, chat)
	wire.ChatArchiveSetAddIsArchived(builder, isArchived)
	builder.Finish(wire.ChatArchiveSetEnd(builder))

	return builder.FinishedBytes()
}

func DecodeDraftSet(body []byte) (string, string, error) {
	var chatID, text string

	err := safely(func() {
		table := wire.GetRootAsDraftSet(body, 0)
		chatID, text = string(table.ChatId()), string(table.Text())
	})

	return chatID, text, err
}

func EncodeDraftSet(chatID, text string) []byte {
	builder := flatbuffers.NewBuilder(len(text) + 64)
	chat := builder.CreateString(chatID)
	body := builder.CreateString(text)
	wire.DraftSetStart(builder)
	wire.DraftSetAddChatId(builder, chat)
	wire.DraftSetAddText(builder, body)
	builder.Finish(wire.DraftSetEnd(builder))

	return builder.FinishedBytes()
}

func EncodeDraftData(data DraftData) []byte {
	builder := flatbuffers.NewBuilder(len(data.Text) + 64)
	chat := builder.CreateString(data.ChatID)
	body := builder.CreateString(data.Text)
	wire.DraftDataStart(builder)
	wire.DraftDataAddChatId(builder, chat)
	wire.DraftDataAddText(builder, body)
	wire.DraftDataAddUpdatedAtUnixMs(builder, data.UpdatedAtMs)
	builder.Finish(wire.DraftDataEnd(builder))

	return builder.FinishedBytes()
}

func DecodeDraftData(body []byte) (DraftData, error) {
	var data DraftData

	err := safely(func() {
		table := wire.GetRootAsDraftData(body, 0)
		data = DraftData{
			ChatID: string(table.ChatId()), Text: string(table.Text()), UpdatedAtMs: table.UpdatedAtUnixMs(),
		}
	})

	return data, err
}
