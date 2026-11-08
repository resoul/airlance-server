package codec

import (
	flatbuffers "github.com/google/flatbuffers/go"

	"go.airlance.org/server/internal/transport/wire"
)

const (
	KindChatLogGet  = Kind(wire.MessageKindChatLogGet)
	KindChatLogData = Kind(wire.MessageKindChatLogData)
)

type ChatLogEntry struct {
	ID           int64
	ActorSubject string
	ActorName    string
	Kind         int32
	Detail       string
	AtUnixMs     int64
}

func DecodeChatLogGet(body []byte) (string, error) {
	var id string

	err := safely(func() {
		id = string(wire.GetRootAsChatLogGet(body, 0).ChatId())
	})

	return id, err
}

func EncodeChatLogData(entries []ChatLogEntry) []byte {
	builder := flatbuffers.NewBuilder(1024)
	offsets := make([]flatbuffers.UOffsetT, len(entries))

	for index, next := range entries {
		subject := builder.CreateString(next.ActorSubject)
		name := builder.CreateString(next.ActorName)
		detail := builder.CreateString(next.Detail)
		wire.ChatLogEntryDataStart(builder)
		wire.ChatLogEntryDataAddId(builder, next.ID)
		wire.ChatLogEntryDataAddActorSubject(builder, subject)
		wire.ChatLogEntryDataAddActorName(builder, name)
		wire.ChatLogEntryDataAddKind(builder, next.Kind)
		wire.ChatLogEntryDataAddDetail(builder, detail)
		wire.ChatLogEntryDataAddAtUnixMs(builder, next.AtUnixMs)
		offsets[index] = wire.ChatLogEntryDataEnd(builder)
	}

	wire.ChatLogDataStartEntriesVector(builder, len(offsets))

	for index := len(offsets) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(offsets[index])
	}

	vector := builder.EndVector(len(offsets))
	wire.ChatLogDataStart(builder)
	wire.ChatLogDataAddEntries(builder, vector)
	builder.Finish(wire.ChatLogDataEnd(builder))

	return builder.FinishedBytes()
}

const (
	KindChatReadersGet = Kind(wire.MessageKindChatReadersGet)
	KindTypingSet      = Kind(wire.MessageKindTypingSet)
	KindTypingEvent    = Kind(wire.MessageKindTypingEvent)
)

func DecodeChatReadersGet(body []byte) (string, int64, error) {
	var (
		id        string
		messageID int64
	)

	err := safely(func() {
		table := wire.GetRootAsChatReadersGet(body, 0)
		id, messageID = string(table.ChatId()), table.MessageId()
	})

	return id, messageID, err
}

func DecodeTypingSet(body []byte) (string, int64, error) {
	var (
		id     string
		thread int64
	)

	err := safely(func() {
		table := wire.GetRootAsTypingSet(body, 0)
		id, thread = string(table.ChatId()), table.ThreadId()
	})

	return id, thread, err
}

func EncodeTypingEvent(chatID string, thread int64, subject, name string) []byte {
	builder := flatbuffers.NewBuilder(128)
	chat := builder.CreateString(chatID)
	who := builder.CreateString(subject)
	label := builder.CreateString(name)
	wire.TypingEventStart(builder)
	wire.TypingEventAddChatId(builder, chat)
	wire.TypingEventAddThreadId(builder, thread)
	wire.TypingEventAddSubject(builder, who)
	wire.TypingEventAddName(builder, label)
	builder.Finish(wire.TypingEventEnd(builder))

	return builder.FinishedBytes()
}
