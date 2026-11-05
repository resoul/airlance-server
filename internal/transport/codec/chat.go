package codec

import (
	flatbuffers "github.com/google/flatbuffers/go"

	"go.airlance.org/server/internal/transport/wire"
)

const maxListItems = 1024

const (
	KindChatOpenDirect = Kind(wire.MessageKindChatOpenDirect)
	KindChatData       = Kind(wire.MessageKindChatData)
	KindMessageSend    = Kind(wire.MessageKindMessageSend)
	KindMessageData    = Kind(wire.MessageKindMessageData)
	KindHistoryGet     = Kind(wire.MessageKindHistoryGet)
	KindHistoryData    = Kind(wire.MessageKindHistoryData)
	KindChatsGet       = Kind(wire.MessageKindChatsGet)
	KindChatsData      = Kind(wire.MessageKindChatsData)
)

type MessageData struct {
	ChatID       string
	MessageID    int64
	Sender       string
	ClientID     string
	Text         string
	SentAtUnixMs int64
}

type ChatData struct {
	ChatID           string
	PeerSubject      string
	PeerFirstName    string
	PeerLastName     string
	PeerUsername     string
	PeerPhotoMediaID string
	PeerVideoMediaID string
	Last             *MessageData
}

type MessageSend struct {
	ChatID   string
	ClientID string
	Text     string
}

type HistoryGet struct {
	ChatID   string
	BeforeID int64
	Limit    int32
}

func DecodeChatOpenDirect(body []byte) (string, error) {
	var peer string

	err := safely(func() {
		peer = string(wire.GetRootAsChatOpenDirect(body, 0).PeerSubject())
	})

	return peer, err
}

func EncodeChatOpenDirect(peer string) []byte {
	builder := flatbuffers.NewBuilder(len(peer) + 32)
	text := builder.CreateString(peer)
	wire.ChatOpenDirectStart(builder)
	wire.ChatOpenDirectAddPeerSubject(builder, text)
	builder.Finish(wire.ChatOpenDirectEnd(builder))

	return builder.FinishedBytes()
}

func DecodeMessageSend(body []byte) (MessageSend, error) {
	var send MessageSend

	err := safely(func() {
		table := wire.GetRootAsMessageSend(body, 0)
		send = MessageSend{
			ChatID:   string(table.ChatId()),
			ClientID: string(table.ClientId()),
			Text:     string(table.Text()),
		}
	})

	return send, err
}

func EncodeMessageSend(send MessageSend) []byte {
	builder := flatbuffers.NewBuilder(len(send.Text) + 128)
	chat := builder.CreateString(send.ChatID)
	client := builder.CreateString(send.ClientID)
	text := builder.CreateString(send.Text)
	wire.MessageSendStart(builder)
	wire.MessageSendAddChatId(builder, chat)
	wire.MessageSendAddClientId(builder, client)
	wire.MessageSendAddText(builder, text)
	builder.Finish(wire.MessageSendEnd(builder))

	return builder.FinishedBytes()
}

func DecodeHistoryGet(body []byte) (HistoryGet, error) {
	var get HistoryGet

	err := safely(func() {
		table := wire.GetRootAsHistoryGet(body, 0)
		get = HistoryGet{ChatID: string(table.ChatId()), BeforeID: table.BeforeId(), Limit: table.Limit()}
	})

	return get, err
}

func EncodeHistoryGet(get HistoryGet) []byte {
	builder := flatbuffers.NewBuilder(len(get.ChatID) + 64)
	chat := builder.CreateString(get.ChatID)
	wire.HistoryGetStart(builder)
	wire.HistoryGetAddChatId(builder, chat)
	wire.HistoryGetAddBeforeId(builder, get.BeforeID)
	wire.HistoryGetAddLimit(builder, get.Limit)
	builder.Finish(wire.HistoryGetEnd(builder))

	return builder.FinishedBytes()
}

func DecodeChatsGet(body []byte) (int64, error) {
	var before int64

	err := safely(func() {
		before = wire.GetRootAsChatsGet(body, 0).BeforeId()
	})

	return before, err
}

func EncodeChatsGet(before int64) []byte {
	builder := flatbuffers.NewBuilder(32)
	wire.ChatsGetStart(builder)
	wire.ChatsGetAddBeforeId(builder, before)
	builder.Finish(wire.ChatsGetEnd(builder))

	return builder.FinishedBytes()
}

func buildMessage(builder *flatbuffers.Builder, data MessageData) flatbuffers.UOffsetT {
	chat := builder.CreateString(data.ChatID)
	sender := builder.CreateString(data.Sender)
	client := builder.CreateString(data.ClientID)
	text := builder.CreateString(data.Text)
	wire.MessageDataStart(builder)
	wire.MessageDataAddChatId(builder, chat)
	wire.MessageDataAddMessageId(builder, data.MessageID)
	wire.MessageDataAddSender(builder, sender)
	wire.MessageDataAddClientId(builder, client)
	wire.MessageDataAddText(builder, text)
	wire.MessageDataAddSentAtUnixMs(builder, data.SentAtUnixMs)

	return wire.MessageDataEnd(builder)
}

func readMessage(table *wire.MessageData) MessageData {
	return MessageData{
		ChatID:       string(table.ChatId()),
		MessageID:    table.MessageId(),
		Sender:       string(table.Sender()),
		ClientID:     string(table.ClientId()),
		Text:         string(table.Text()),
		SentAtUnixMs: table.SentAtUnixMs(),
	}
}

func EncodeMessageData(data MessageData) []byte {
	builder := flatbuffers.NewBuilder(len(data.Text) + 256)
	builder.Finish(buildMessage(builder, data))

	return builder.FinishedBytes()
}

func DecodeMessageData(body []byte) (MessageData, error) {
	var data MessageData

	err := safely(func() {
		data = readMessage(wire.GetRootAsMessageData(body, 0))
	})

	return data, err
}

func buildChat(builder *flatbuffers.Builder, data ChatData) flatbuffers.UOffsetT {
	var last flatbuffers.UOffsetT
	if data.Last != nil {
		last = buildMessage(builder, *data.Last)
	}

	chat := builder.CreateString(data.ChatID)
	subject := builder.CreateString(data.PeerSubject)
	first := builder.CreateString(data.PeerFirstName)
	lastName := builder.CreateString(data.PeerLastName)
	username := builder.CreateString(data.PeerUsername)
	photo := builder.CreateString(data.PeerPhotoMediaID)
	video := builder.CreateString(data.PeerVideoMediaID)
	wire.ChatDataStart(builder)
	wire.ChatDataAddChatId(builder, chat)
	wire.ChatDataAddPeerSubject(builder, subject)
	wire.ChatDataAddPeerFirstName(builder, first)
	wire.ChatDataAddPeerLastName(builder, lastName)
	wire.ChatDataAddPeerUsername(builder, username)
	wire.ChatDataAddPeerPhotoMediaId(builder, photo)
	wire.ChatDataAddPeerVideoMediaId(builder, video)
	if data.Last != nil {
		wire.ChatDataAddLastMessage(builder, last)
	}

	return wire.ChatDataEnd(builder)
}

func readChat(table *wire.ChatData) ChatData {
	data := ChatData{
		ChatID:           string(table.ChatId()),
		PeerSubject:      string(table.PeerSubject()),
		PeerFirstName:    string(table.PeerFirstName()),
		PeerLastName:     string(table.PeerLastName()),
		PeerUsername:     string(table.PeerUsername()),
		PeerPhotoMediaID: string(table.PeerPhotoMediaId()),
		PeerVideoMediaID: string(table.PeerVideoMediaId()),
	}

	if last := table.LastMessage(nil); last != nil {
		message := readMessage(last)
		data.Last = &message
	}

	return data
}

func EncodeChatData(data ChatData) []byte {
	builder := flatbuffers.NewBuilder(256)
	builder.Finish(buildChat(builder, data))

	return builder.FinishedBytes()
}

func DecodeChatData(body []byte) (ChatData, error) {
	var data ChatData

	err := safely(func() {
		data = readChat(wire.GetRootAsChatData(body, 0))
	})

	return data, err
}

func EncodeHistoryData(messages []MessageData) []byte {
	builder := flatbuffers.NewBuilder(1024)
	offsets := make([]flatbuffers.UOffsetT, len(messages))

	for index, next := range messages {
		offsets[index] = buildMessage(builder, next)
	}

	wire.HistoryDataStartMessagesVector(builder, len(offsets))

	for index := len(offsets) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(offsets[index])
	}

	vector := builder.EndVector(len(offsets))
	wire.HistoryDataStart(builder)
	wire.HistoryDataAddMessages(builder, vector)
	builder.Finish(wire.HistoryDataEnd(builder))

	return builder.FinishedBytes()
}

func DecodeHistoryData(body []byte) ([]MessageData, error) {
	var messages []MessageData

	err := safely(func() {
		table := wire.GetRootAsHistoryData(body, 0)
		if table.MessagesLength() > maxListItems {
			panic("too many messages")
		}

		messages = make([]MessageData, table.MessagesLength())

		var row wire.MessageData

		for index := range messages {
			if !table.Messages(&row, index) {
				panic("message")
			}

			messages[index] = readMessage(&row)
		}
	})

	return messages, err
}

func EncodeChatsData(chats []ChatData) []byte {
	builder := flatbuffers.NewBuilder(1024)
	offsets := make([]flatbuffers.UOffsetT, len(chats))

	for index, next := range chats {
		offsets[index] = buildChat(builder, next)
	}

	wire.ChatsDataStartChatsVector(builder, len(offsets))

	for index := len(offsets) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(offsets[index])
	}

	vector := builder.EndVector(len(offsets))
	wire.ChatsDataStart(builder)
	wire.ChatsDataAddChats(builder, vector)
	builder.Finish(wire.ChatsDataEnd(builder))

	return builder.FinishedBytes()
}

func DecodeChatsData(body []byte) ([]ChatData, error) {
	var chats []ChatData

	err := safely(func() {
		table := wire.GetRootAsChatsData(body, 0)
		if table.ChatsLength() > maxListItems {
			panic("too many chats")
		}

		chats = make([]ChatData, table.ChatsLength())

		var row wire.ChatData

		for index := range chats {
			if !table.Chats(&row, index) {
				panic("chat")
			}

			chats[index] = readChat(&row)
		}
	})

	return chats, err
}
