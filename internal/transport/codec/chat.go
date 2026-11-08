package codec

import (
	flatbuffers "github.com/google/flatbuffers/go"

	"go.airlance.org/server/internal/transport/wire"
)

const (
	maxListItems   = 1024
	maxAttachments = 64
)

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
	FirstName    string
	LastName     string
	PhotoMediaID string
	VideoMediaID string
	AgentName    string
	ForwardName  string
	Attachments  []Attachment
	ReplyToID    int64
	Entities     []Entity
	EditedAtMs   int64
	ThreadID     int64
	Reactions    []ReactionCount
	MyReaction   string
	Poll         *Poll
}

type Attachment struct {
	Kind         int32
	MediaID      string
	ThumbMediaID string
	Name         string
	Size         int64
	Width        int32
	Height       int32
	DurationMs   int32
	Waveform     []byte
	Title        string
	Performer    string
	Latitude     float64
	Longitude    float64
	Phone        string
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
	Kind             int32
	Title            string
	About            string
	Link             string
	InviteToken      string
	MemberCount      int32
	Role             int32
	IsMember         bool
	IsPinned         bool
	IsMuted          bool
	MyReadID         int64
	PeerReadID       int64
	PeerDeliveredID  int64
	MemberRights     int64
	MyRights         int64
	Rules            ChatRules
	Unread           int32
	IsArchived       bool
	Draft            string
	DraftAtMs        int64
}

type MessageSend struct {
	ChatID      string
	ClientID    string
	Text        string
	ForwardName string
	Attachments []Attachment
	ReplyToID   int64
	Entities    []Entity
	ThreadID    int64
	Poll        *Poll
}

type HistoryGet struct {
	ChatID   string
	BeforeID int64
	Limit    int32
	ThreadID int64
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
			ChatID:      string(table.ChatId()),
			ClientID:    string(table.ClientId()),
			Text:        string(table.Text()),
			ForwardName: string(table.ForwardName()),
			Attachments: readSendAttachments(table),
			ReplyToID:   table.ReplyToId(),
			Entities:    readEntities(table.EntitiesLength(), table.Entities),
			ThreadID:    table.ThreadId(),
			Poll:        readPoll(table.Poll(nil)),
		}
	})

	return send, err
}

func EncodeMessageSend(send MessageSend) []byte {
	builder := flatbuffers.NewBuilder(len(send.Text) + 128)
	chat := builder.CreateString(send.ChatID)
	client := builder.CreateString(send.ClientID)
	text := builder.CreateString(send.Text)
	forward := builder.CreateString(send.ForwardName)
	attachments := buildAttachments(builder, send.Attachments, wire.MessageSendStartAttachmentsVector)
	entities := buildEntities(builder, send.Entities, wire.MessageSendStartEntitiesVector)
	poll := buildPoll(builder, send.Poll)
	wire.MessageSendStart(builder)
	wire.MessageSendAddReplyToId(builder, send.ReplyToID)
	wire.MessageSendAddThreadId(builder, send.ThreadID)
	wire.MessageSendAddPoll(builder, poll)
	wire.MessageSendAddEntities(builder, entities)
	wire.MessageSendAddForwardName(builder, forward)
	wire.MessageSendAddAttachments(builder, attachments)
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
		get = HistoryGet{
			ChatID: string(table.ChatId()), BeforeID: table.BeforeId(), Limit: table.Limit(), ThreadID: table.ThreadId(),
		}
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
	wire.HistoryGetAddThreadId(builder, get.ThreadID)
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
	first := builder.CreateString(data.FirstName)
	last := builder.CreateString(data.LastName)
	photo := builder.CreateString(data.PhotoMediaID)
	video := builder.CreateString(data.VideoMediaID)
	agentName := builder.CreateString(data.AgentName)
	forward := builder.CreateString(data.ForwardName)
	attachments := buildAttachments(builder, data.Attachments, wire.MessageDataStartAttachmentsVector)
	entities := buildEntities(builder, data.Entities, wire.MessageDataStartEntitiesVector)
	reactions := buildReactions(builder, data.Reactions)
	myReaction := builder.CreateString(data.MyReaction)
	poll := buildPoll(builder, data.Poll)
	wire.MessageDataStart(builder)
	wire.MessageDataAddReplyToId(builder, data.ReplyToID)
	wire.MessageDataAddReactions(builder, reactions)
	wire.MessageDataAddMyReaction(builder, myReaction)
	wire.MessageDataAddPoll(builder, poll)
	wire.MessageDataAddThreadId(builder, data.ThreadID)
	wire.MessageDataAddEntities(builder, entities)
	wire.MessageDataAddEditedAtUnixMs(builder, data.EditedAtMs)
	wire.MessageDataAddForwardName(builder, forward)
	wire.MessageDataAddAttachments(builder, attachments)
	wire.MessageDataAddAgentName(builder, agentName)
	wire.MessageDataAddChatId(builder, chat)
	wire.MessageDataAddMessageId(builder, data.MessageID)
	wire.MessageDataAddSender(builder, sender)
	wire.MessageDataAddClientId(builder, client)
	wire.MessageDataAddText(builder, text)
	wire.MessageDataAddSentAtUnixMs(builder, data.SentAtUnixMs)
	wire.MessageDataAddSenderFirstName(builder, first)
	wire.MessageDataAddSenderLastName(builder, last)
	wire.MessageDataAddSenderPhotoMediaId(builder, photo)
	wire.MessageDataAddSenderVideoMediaId(builder, video)

	return wire.MessageDataEnd(builder)
}

func buildAttachments(
	builder *flatbuffers.Builder,
	attachments []Attachment,
	start func(*flatbuffers.Builder, int) flatbuffers.UOffsetT,
) flatbuffers.UOffsetT {
	offsets := make([]flatbuffers.UOffsetT, len(attachments))

	for index, next := range attachments {
		media := builder.CreateString(next.MediaID)
		thumb := builder.CreateString(next.ThumbMediaID)
		name := builder.CreateString(next.Name)
		waveform := builder.CreateByteVector(next.Waveform)
		title := builder.CreateString(next.Title)
		performer := builder.CreateString(next.Performer)
		phone := builder.CreateString(next.Phone)
		wire.MessageAttachmentStart(builder)
		wire.MessageAttachmentAddKind(builder, next.Kind)
		wire.MessageAttachmentAddMediaId(builder, media)
		wire.MessageAttachmentAddThumbMediaId(builder, thumb)
		wire.MessageAttachmentAddName(builder, name)
		wire.MessageAttachmentAddSize(builder, next.Size)
		wire.MessageAttachmentAddWidth(builder, next.Width)
		wire.MessageAttachmentAddHeight(builder, next.Height)
		wire.MessageAttachmentAddDurationMs(builder, next.DurationMs)
		wire.MessageAttachmentAddWaveform(builder, waveform)
		wire.MessageAttachmentAddTitle(builder, title)
		wire.MessageAttachmentAddPerformer(builder, performer)
		wire.MessageAttachmentAddLatitude(builder, next.Latitude)
		wire.MessageAttachmentAddLongitude(builder, next.Longitude)
		wire.MessageAttachmentAddPhone(builder, phone)
		offsets[index] = wire.MessageAttachmentEnd(builder)
	}

	start(builder, len(offsets))

	for index := len(offsets) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(offsets[index])
	}

	return builder.EndVector(len(offsets))
}

func readAttachment(row *wire.MessageAttachment) Attachment {
	return Attachment{
		Kind:         row.Kind(),
		MediaID:      string(row.MediaId()),
		ThumbMediaID: string(row.ThumbMediaId()),
		Name:         string(row.Name()),
		Size:         row.Size(),
		Width:        row.Width(),
		Height:       row.Height(),
		DurationMs:   row.DurationMs(),
		Waveform:     append([]byte(nil), row.WaveformBytes()...),
		Title:        string(row.Title()),
		Performer:    string(row.Performer()),
		Latitude:     row.Latitude(),
		Longitude:    row.Longitude(),
		Phone:        string(row.Phone()),
	}
}

func readSendAttachments(table *wire.MessageSend) []Attachment {
	if table.AttachmentsLength() > maxAttachments {
		panic("too many attachments")
	}

	if table.AttachmentsLength() == 0 {
		return nil
	}

	attachments := make([]Attachment, table.AttachmentsLength())

	var row wire.MessageAttachment

	for index := range attachments {
		if !table.Attachments(&row, index) {
			panic("attachment")
		}

		attachments[index] = readAttachment(&row)
	}

	return attachments
}

func readDataAttachments(table *wire.MessageData) []Attachment {
	if table.AttachmentsLength() > maxAttachments {
		panic("too many attachments")
	}

	if table.AttachmentsLength() == 0 {
		return nil
	}

	attachments := make([]Attachment, table.AttachmentsLength())

	var row wire.MessageAttachment

	for index := range attachments {
		if !table.Attachments(&row, index) {
			panic("attachment")
		}

		attachments[index] = readAttachment(&row)
	}

	return attachments
}

func readMessage(table *wire.MessageData) MessageData {
	return MessageData{
		ChatID:       string(table.ChatId()),
		MessageID:    table.MessageId(),
		Sender:       string(table.Sender()),
		ClientID:     string(table.ClientId()),
		Text:         string(table.Text()),
		SentAtUnixMs: table.SentAtUnixMs(),
		FirstName:    string(table.SenderFirstName()),
		LastName:     string(table.SenderLastName()),
		PhotoMediaID: string(table.SenderPhotoMediaId()),
		VideoMediaID: string(table.SenderVideoMediaId()),
		AgentName:    string(table.AgentName()),
		ForwardName:  string(table.ForwardName()),
		Attachments:  readDataAttachments(table),
		ReplyToID:    table.ReplyToId(),
		Entities:     readEntities(table.EntitiesLength(), table.Entities),
		EditedAtMs:   table.EditedAtUnixMs(),
		ThreadID:     table.ThreadId(),
		Reactions:    readReactions(table),
		MyReaction:   string(table.MyReaction()),
		Poll:         readPoll(table.Poll(nil)),
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
	title := builder.CreateString(data.Title)
	about := builder.CreateString(data.About)
	link := builder.CreateString(data.Link)
	invite := builder.CreateString(data.InviteToken)
	draft := builder.CreateString(data.Draft)
	rules := buildRules(builder, data.Rules)
	wire.ChatDataStart(builder)
	wire.ChatDataAddKind(builder, data.Kind)
	wire.ChatDataAddTitle(builder, title)
	wire.ChatDataAddAbout(builder, about)
	wire.ChatDataAddLink(builder, link)
	wire.ChatDataAddInviteToken(builder, invite)
	wire.ChatDataAddMemberCount(builder, data.MemberCount)
	wire.ChatDataAddRole(builder, data.Role)
	wire.ChatDataAddIsMember(builder, data.IsMember)
	wire.ChatDataAddIsPinned(builder, data.IsPinned)
	wire.ChatDataAddIsMuted(builder, data.IsMuted)
	wire.ChatDataAddMyReadId(builder, data.MyReadID)
	wire.ChatDataAddPeerReadId(builder, data.PeerReadID)
	wire.ChatDataAddUnread(builder, data.Unread)
	wire.ChatDataAddIsArchived(builder, data.IsArchived)
	wire.ChatDataAddDraft(builder, draft)
	wire.ChatDataAddDraftAtUnixMs(builder, data.DraftAtMs)
	wire.ChatDataAddPeerDeliveredId(builder, data.PeerDeliveredID)
	wire.ChatDataAddMemberRights(builder, data.MemberRights)
	wire.ChatDataAddMyRights(builder, data.MyRights)
	wire.ChatDataAddRules(builder, rules)
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
		Kind:             table.Kind(),
		Title:            string(table.Title()),
		About:            string(table.About()),
		Link:             string(table.Link()),
		InviteToken:      string(table.InviteToken()),
		MemberCount:      table.MemberCount(),
		Role:             table.Role(),
		IsMember:         table.IsMember(),
		IsPinned:         table.IsPinned(),
		IsMuted:          table.IsMuted(),
		MyReadID:         table.MyReadId(),
		PeerReadID:       table.PeerReadId(),
		Unread:           table.Unread(),
		IsArchived:       table.IsArchived(),
		Draft:            string(table.Draft()),
		DraftAtMs:        table.DraftAtUnixMs(),
		PeerDeliveredID:  table.PeerDeliveredId(),
		MemberRights:     table.MemberRights(),
		MyRights:         table.MyRights(),
		Rules:            readRules(table.Rules(nil)),
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

type ChatRules struct {
	NoCopy        bool
	Signs         bool
	ReactionsMode uint8
	Reactions     []string
	PerPost       uint8
}

func buildRules(builder *flatbuffers.Builder, rules ChatRules) flatbuffers.UOffsetT {
	offsets := make([]flatbuffers.UOffsetT, len(rules.Reactions))
	for index, emoji := range rules.Reactions {
		offsets[index] = builder.CreateString(emoji)
	}

	wire.ChatRulesStartReactionsVector(builder, len(offsets))

	for index := len(offsets) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(offsets[index])
	}

	reactions := builder.EndVector(len(offsets))

	wire.ChatRulesStart(builder)
	wire.ChatRulesAddNoCopy(builder, rules.NoCopy)
	wire.ChatRulesAddSigns(builder, rules.Signs)
	wire.ChatRulesAddReactionsMode(builder, rules.ReactionsMode)
	wire.ChatRulesAddReactions(builder, reactions)
	wire.ChatRulesAddReactionsPerPost(builder, rules.PerPost)

	return wire.ChatRulesEnd(builder)
}

func readRules(table *wire.ChatRules) ChatRules {
	if table == nil {
		return ChatRules{}
	}

	rules := ChatRules{
		NoCopy: table.NoCopy(), Signs: table.Signs(),
		ReactionsMode: table.ReactionsMode(), PerPost: table.ReactionsPerPost(),
	}

	for index := range table.ReactionsLength() {
		rules.Reactions = append(rules.Reactions, string(table.Reactions(index)))
	}

	return rules
}
