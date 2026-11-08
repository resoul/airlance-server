package codec

import (
	flatbuffers "github.com/google/flatbuffers/go"

	"go.airlance.org/server/internal/transport/wire"
)

const (
	KindMessagePinSet = Kind(wire.MessageKindMessagePinSet)
	KindPinsGet       = Kind(wire.MessageKindPinsGet)
	KindPinsData      = Kind(wire.MessageKindPinsData)
	KindPostStatsGet  = Kind(wire.MessageKindPostStatsGet)
	KindPostStatsData = Kind(wire.MessageKindPostStatsData)

	maxStatItems = 100
)

type MessagePinSet struct {
	ChatID    string
	MessageID int64
	IsPinned  bool
}

type PostCommenter struct {
	Subject      string
	FirstName    string
	LastName     string
	PhotoMediaID string
	VideoMediaID string
}

type PostStat struct {
	MessageID int64
	Views     int32
	Comments  int32
	Recent    []PostCommenter
}

type PostStatsGet struct {
	ChatID     string
	MessageIDs []int64
	MarkViewed bool
}

func DecodeMessagePinSet(body []byte) (MessagePinSet, error) {
	var set MessagePinSet

	err := safely(func() {
		table := wire.GetRootAsMessagePinSet(body, 0)
		set = MessagePinSet{ChatID: string(table.ChatId()), MessageID: table.MessageId(), IsPinned: table.IsPinned()}
	})

	return set, err
}

func EncodeMessagePinSet(set MessagePinSet) []byte {
	builder := flatbuffers.NewBuilder(96)
	chat := builder.CreateString(set.ChatID)
	wire.MessagePinSetStart(builder)
	wire.MessagePinSetAddChatId(builder, chat)
	wire.MessagePinSetAddMessageId(builder, set.MessageID)
	wire.MessagePinSetAddIsPinned(builder, set.IsPinned)
	builder.Finish(wire.MessagePinSetEnd(builder))

	return builder.FinishedBytes()
}

func DecodePinsGet(body []byte) (string, error) {
	var chatID string

	err := safely(func() {
		chatID = string(wire.GetRootAsPinsGet(body, 0).ChatId())
	})

	return chatID, err
}

func EncodePinsGet(chatID string) []byte {
	builder := flatbuffers.NewBuilder(64)
	chat := builder.CreateString(chatID)
	wire.PinsGetStart(builder)
	wire.PinsGetAddChatId(builder, chat)
	builder.Finish(wire.PinsGetEnd(builder))

	return builder.FinishedBytes()
}

func EncodePinsData(chatID string, ids []int64) []byte {
	builder := flatbuffers.NewBuilder(96)
	chat := builder.CreateString(chatID)
	vector := buildIDs(builder, ids, wire.PinsDataStartMessageIdsVector)
	wire.PinsDataStart(builder)
	wire.PinsDataAddChatId(builder, chat)
	wire.PinsDataAddMessageIds(builder, vector)
	builder.Finish(wire.PinsDataEnd(builder))

	return builder.FinishedBytes()
}

func DecodePinsData(body []byte) (string, []int64, error) {
	var (
		chatID string
		ids    []int64
	)

	err := safely(func() {
		table := wire.GetRootAsPinsData(body, 0)
		chatID = string(table.ChatId())
		ids = readIDs(table.MessageIdsLength(), table.MessageIds)
	})

	return chatID, ids, err
}

func DecodePostStatsGet(body []byte) (PostStatsGet, error) {
	var get PostStatsGet

	err := safely(func() {
		table := wire.GetRootAsPostStatsGet(body, 0)
		get = PostStatsGet{
			ChatID:     string(table.ChatId()),
			MessageIDs: readIDs(table.MessageIdsLength(), table.MessageIds),
			MarkViewed: table.MarkViewed(),
		}
	})

	return get, err
}

func EncodePostStatsGet(get PostStatsGet) []byte {
	builder := flatbuffers.NewBuilder(128)
	chat := builder.CreateString(get.ChatID)
	ids := buildIDs(builder, get.MessageIDs, wire.PostStatsGetStartMessageIdsVector)
	wire.PostStatsGetStart(builder)
	wire.PostStatsGetAddChatId(builder, chat)
	wire.PostStatsGetAddMessageIds(builder, ids)
	wire.PostStatsGetAddMarkViewed(builder, get.MarkViewed)
	builder.Finish(wire.PostStatsGetEnd(builder))

	return builder.FinishedBytes()
}

func EncodePostStatsData(chatID string, items []PostStat) []byte {
	builder := flatbuffers.NewBuilder(256)
	chat := builder.CreateString(chatID)
	offsets := make([]flatbuffers.UOffsetT, len(items))

	for index, next := range items {
		offsets[index] = buildPostStat(builder, next)
	}

	wire.PostStatsDataStartItemsVector(builder, len(offsets))

	for index := len(offsets) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(offsets[index])
	}

	vector := builder.EndVector(len(offsets))
	wire.PostStatsDataStart(builder)
	wire.PostStatsDataAddChatId(builder, chat)
	wire.PostStatsDataAddItems(builder, vector)
	builder.Finish(wire.PostStatsDataEnd(builder))

	return builder.FinishedBytes()
}

func buildPostStat(builder *flatbuffers.Builder, item PostStat) flatbuffers.UOffsetT {
	people := make([]flatbuffers.UOffsetT, len(item.Recent))

	for index, next := range item.Recent {
		subject := builder.CreateString(next.Subject)
		first := builder.CreateString(next.FirstName)
		last := builder.CreateString(next.LastName)
		photo := builder.CreateString(next.PhotoMediaID)
		video := builder.CreateString(next.VideoMediaID)
		wire.PostCommenterStart(builder)
		wire.PostCommenterAddSubject(builder, subject)
		wire.PostCommenterAddFirstName(builder, first)
		wire.PostCommenterAddLastName(builder, last)
		wire.PostCommenterAddPhotoMediaId(builder, photo)
		wire.PostCommenterAddVideoMediaId(builder, video)
		people[index] = wire.PostCommenterEnd(builder)
	}

	wire.PostStatStartRecentVector(builder, len(people))

	for index := len(people) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(people[index])
	}

	recent := builder.EndVector(len(people))
	wire.PostStatStart(builder)
	wire.PostStatAddMessageId(builder, item.MessageID)
	wire.PostStatAddViews(builder, item.Views)
	wire.PostStatAddComments(builder, item.Comments)
	wire.PostStatAddRecent(builder, recent)

	return wire.PostStatEnd(builder)
}

func DecodePostStatsData(body []byte) (string, []PostStat, error) {
	var (
		chatID string
		items  []PostStat
	)

	err := safely(func() {
		table := wire.GetRootAsPostStatsData(body, 0)
		if table.ItemsLength() > maxStatItems {
			panic("too many stats")
		}

		chatID = string(table.ChatId())
		items = make([]PostStat, table.ItemsLength())

		var row wire.PostStat

		for index := range items {
			if !table.Items(&row, index) {
				panic("stat")
			}

			items[index] = readPostStat(&row)
		}
	})

	return chatID, items, err
}

func readPostStat(row *wire.PostStat) PostStat {
	item := PostStat{MessageID: row.MessageId(), Views: row.Views(), Comments: row.Comments()}
	if row.RecentLength() > maxStatItems {
		panic("too many commenters")
	}

	var person wire.PostCommenter

	for index := range row.RecentLength() {
		if !row.Recent(&person, index) {
			panic("commenter")
		}

		item.Recent = append(item.Recent, PostCommenter{
			Subject:      string(person.Subject()),
			FirstName:    string(person.FirstName()),
			LastName:     string(person.LastName()),
			PhotoMediaID: string(person.PhotoMediaId()),
			VideoMediaID: string(person.VideoMediaId()),
		})
	}

	return item
}
