package codec

import (
	flatbuffers "github.com/google/flatbuffers/go"

	"go.airlance.org/server/internal/transport/wire"
)

const (
	KindChatReadSet = Kind(wire.MessageKindChatReadSet)
	KindReadData    = Kind(wire.MessageKindReadData)

	KindChatDeliveredSet = Kind(wire.MessageKindChatDeliveredSet)
	KindReactionSet      = Kind(wire.MessageKindReactionSet)

	maxReactionKinds = 256
)

type ReactionCount struct {
	Emoji string
	Count int32
}

type ReadData struct {
	ChatID     string
	MyReadID   int64
	PeerReadID int64
	Unread     int32

	PeerDeliveredID int64
}

type ReactionSet struct {
	ChatID    string
	MessageID int64
	Emoji     string
}

func buildReactions(builder *flatbuffers.Builder, counts []ReactionCount) flatbuffers.UOffsetT {
	offsets := make([]flatbuffers.UOffsetT, len(counts))

	for index, next := range counts {
		emoji := builder.CreateString(next.Emoji)
		wire.ReactionCountStart(builder)
		wire.ReactionCountAddEmoji(builder, emoji)
		wire.ReactionCountAddCount(builder, next.Count)
		offsets[index] = wire.ReactionCountEnd(builder)
	}

	wire.MessageDataStartReactionsVector(builder, len(offsets))

	for index := len(offsets) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(offsets[index])
	}

	return builder.EndVector(len(offsets))
}

func readReactions(table *wire.MessageData) []ReactionCount {
	if table.ReactionsLength() > maxReactionKinds {
		panic("too many reactions")
	}

	if table.ReactionsLength() == 0 {
		return nil
	}

	counts := make([]ReactionCount, table.ReactionsLength())

	var row wire.ReactionCount

	for index := range counts {
		if !table.Reactions(&row, index) {
			panic("reaction")
		}

		counts[index] = ReactionCount{Emoji: string(row.Emoji()), Count: row.Count()}
	}

	return counts
}

func DecodeChatReadSet(body []byte) (string, int64, error) {
	var (
		chatID string
		upTo   int64
	)

	err := safely(func() {
		table := wire.GetRootAsChatReadSet(body, 0)
		chatID, upTo = string(table.ChatId()), table.UpToId()
	})

	return chatID, upTo, err
}

func EncodeChatReadSet(chatID string, upTo int64) []byte {
	builder := flatbuffers.NewBuilder(64)
	chat := builder.CreateString(chatID)
	wire.ChatReadSetStart(builder)
	wire.ChatReadSetAddChatId(builder, chat)
	wire.ChatReadSetAddUpToId(builder, upTo)
	builder.Finish(wire.ChatReadSetEnd(builder))

	return builder.FinishedBytes()
}

func EncodeReadData(data ReadData) []byte {
	builder := flatbuffers.NewBuilder(64)
	chat := builder.CreateString(data.ChatID)
	wire.ReadDataStart(builder)
	wire.ReadDataAddChatId(builder, chat)
	wire.ReadDataAddMyReadId(builder, data.MyReadID)
	wire.ReadDataAddPeerReadId(builder, data.PeerReadID)
	wire.ReadDataAddUnread(builder, data.Unread)
	wire.ReadDataAddPeerDeliveredId(builder, data.PeerDeliveredID)
	builder.Finish(wire.ReadDataEnd(builder))

	return builder.FinishedBytes()
}

func DecodeReadData(body []byte) (ReadData, error) {
	var data ReadData

	err := safely(func() {
		table := wire.GetRootAsReadData(body, 0)
		data = ReadData{
			ChatID:     string(table.ChatId()),
			MyReadID:   table.MyReadId(),
			PeerReadID: table.PeerReadId(),
			Unread:     table.Unread(),

			PeerDeliveredID: table.PeerDeliveredId(),
		}
	})

	return data, err
}

func DecodeReactionSet(body []byte) (ReactionSet, error) {
	var set ReactionSet

	err := safely(func() {
		table := wire.GetRootAsReactionSet(body, 0)
		set = ReactionSet{ChatID: string(table.ChatId()), MessageID: table.MessageId(), Emoji: string(table.Emoji())}
	})

	return set, err
}

func EncodeReactionSet(set ReactionSet) []byte {
	builder := flatbuffers.NewBuilder(96)
	chat := builder.CreateString(set.ChatID)
	emoji := builder.CreateString(set.Emoji)
	wire.ReactionSetStart(builder)
	wire.ReactionSetAddChatId(builder, chat)
	wire.ReactionSetAddMessageId(builder, set.MessageID)
	wire.ReactionSetAddEmoji(builder, emoji)
	builder.Finish(wire.ReactionSetEnd(builder))

	return builder.FinishedBytes()
}

func DecodeChatDeliveredSet(body []byte) (string, int64, error) {
	var (
		chatID string
		upTo   int64
	)

	err := safely(func() {
		table := wire.GetRootAsChatDeliveredSet(body, 0)
		chatID, upTo = string(table.ChatId()), table.UpToId()
	})

	return chatID, upTo, err
}

func EncodeChatDeliveredSet(chatID string, upTo int64) []byte {
	builder := flatbuffers.NewBuilder(64)
	chat := builder.CreateString(chatID)
	wire.ChatDeliveredSetStart(builder)
	wire.ChatDeliveredSetAddChatId(builder, chat)
	wire.ChatDeliveredSetAddUpToId(builder, upTo)
	builder.Finish(wire.ChatDeliveredSetEnd(builder))

	return builder.FinishedBytes()
}
