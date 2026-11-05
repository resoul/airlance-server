package codec

import (
	flatbuffers "github.com/google/flatbuffers/go"

	"go.airlance.org/server/internal/transport/wire"
)

const maxInvitedPeople = 200

const (
	KindChatCreate       = Kind(wire.MessageKindChatCreate)
	KindChatJoin         = Kind(wire.MessageKindChatJoin)
	KindChatLeave        = Kind(wire.MessageKindChatLeave)
	KindChatMembersGet   = Kind(wire.MessageKindChatMembersGet)
	KindChatMembersData  = Kind(wire.MessageKindChatMembersData)
	KindChatMemberAdd    = Kind(wire.MessageKindChatMemberAdd)
	KindChatMemberRemove = Kind(wire.MessageKindChatMemberRemove)
	KindChatSearch       = Kind(wire.MessageKindChatSearch)
	KindChatSearchData   = Kind(wire.MessageKindChatSearchData)
	KindChatsChanged     = Kind(wire.MessageKindChatsChanged)
	KindChatPinSet       = Kind(wire.MessageKindChatPinSet)
	KindChatMuteSet      = Kind(wire.MessageKindChatMuteSet)
	KindChatResolve      = Kind(wire.MessageKindChatResolve)
	KindChatInviteReset  = Kind(wire.MessageKindChatInviteReset)
	KindUserResolve      = Kind(wire.MessageKindUserResolve)
)

type ChatCreate struct {
	ClientID string
	Kind     int32
	Title    string
	About    string
	Link     string
	Members  []string
}

type ChatMemberData struct {
	Subject      string
	FirstName    string
	LastName     string
	Username     string
	PhotoMediaID string
	VideoMediaID string
	Role         int32
}

type ChatMemberChange struct {
	ChatID  string
	Subject string
	Role    int32
}

func DecodeChatCreate(body []byte) (ChatCreate, error) {
	var create ChatCreate

	err := safely(func() {
		table := wire.GetRootAsChatCreate(body, 0)
		if table.MembersLength() > maxInvitedPeople {
			panic("too many people")
		}

		create = ChatCreate{
			ClientID: string(table.ClientId()),
			Kind:     table.Kind(),
			Title:    string(table.Title()),
			About:    string(table.About()),
			Link:     string(table.Link()),
			Members:  make([]string, table.MembersLength()),
		}
		for index := range create.Members {
			create.Members[index] = string(table.Members(index))
		}
	})

	return create, err
}

func EncodeChatCreate(create ChatCreate) []byte {
	builder := flatbuffers.NewBuilder(256)
	client := builder.CreateString(create.ClientID)
	title := builder.CreateString(create.Title)
	about := builder.CreateString(create.About)
	link := builder.CreateString(create.Link)
	people := make([]flatbuffers.UOffsetT, len(create.Members))

	for index, member := range create.Members {
		people[index] = builder.CreateString(member)
	}

	wire.ChatCreateStartMembersVector(builder, len(people))

	for index := len(people) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(people[index])
	}

	members := builder.EndVector(len(people))
	wire.ChatCreateStart(builder)
	wire.ChatCreateAddClientId(builder, client)
	wire.ChatCreateAddKind(builder, create.Kind)
	wire.ChatCreateAddTitle(builder, title)
	wire.ChatCreateAddAbout(builder, about)
	wire.ChatCreateAddLink(builder, link)
	wire.ChatCreateAddMembers(builder, members)
	builder.Finish(wire.ChatCreateEnd(builder))

	return builder.FinishedBytes()
}

func DecodeChatJoin(body []byte) (string, error) {
	var code string

	err := safely(func() {
		code = string(wire.GetRootAsChatJoin(body, 0).Code())
	})

	return code, err
}

func DecodeChatLeave(body []byte) (string, error) {
	var id string

	err := safely(func() {
		id = string(wire.GetRootAsChatLeave(body, 0).ChatId())
	})

	return id, err
}

func DecodeChatMembersGet(body []byte) (string, error) {
	var id string

	err := safely(func() {
		id = string(wire.GetRootAsChatMembersGet(body, 0).ChatId())
	})

	return id, err
}

func DecodeChatPinSet(body []byte) (string, bool, error) {
	var (
		id       string
		isPinned bool
	)

	err := safely(func() {
		table := wire.GetRootAsChatPinSet(body, 0)
		id, isPinned = string(table.ChatId()), table.IsPinned()
	})

	return id, isPinned, err
}

func DecodeChatMuteSet(body []byte) (string, bool, error) {
	var (
		id      string
		isMuted bool
	)

	err := safely(func() {
		table := wire.GetRootAsChatMuteSet(body, 0)
		id, isMuted = string(table.ChatId()), table.IsMuted()
	})

	return id, isMuted, err
}

func DecodeChatSearch(body []byte) (string, error) {
	var query string

	err := safely(func() {
		query = string(wire.GetRootAsChatSearch(body, 0).Query())
	})

	return query, err
}

func DecodeChatResolve(body []byte) (string, error) {
	var code string

	err := safely(func() {
		code = string(wire.GetRootAsChatResolve(body, 0).Code())
	})

	return code, err
}

func DecodeChatInviteReset(body []byte) (string, error) {
	var id string

	err := safely(func() {
		id = string(wire.GetRootAsChatInviteReset(body, 0).ChatId())
	})

	return id, err
}

func DecodeUserResolve(body []byte) (string, error) {
	var username string

	err := safely(func() {
		username = string(wire.GetRootAsUserResolve(body, 0).Username())
	})

	return username, err
}

func DecodeChatMemberAdd(body []byte) (ChatMemberChange, error) {
	var change ChatMemberChange

	err := safely(func() {
		table := wire.GetRootAsChatMemberAdd(body, 0)
		change = ChatMemberChange{ChatID: string(table.ChatId()), Subject: string(table.Subject()), Role: table.Role()}
	})

	return change, err
}

func DecodeChatMemberRemove(body []byte) (ChatMemberChange, error) {
	var change ChatMemberChange

	err := safely(func() {
		table := wire.GetRootAsChatMemberRemove(body, 0)
		change = ChatMemberChange{ChatID: string(table.ChatId()), Subject: string(table.Subject())}
	})

	return change, err
}

func EncodeChatMembersData(members []ChatMemberData) []byte {
	builder := flatbuffers.NewBuilder(1024)
	offsets := make([]flatbuffers.UOffsetT, len(members))

	for index, next := range members {
		subject := builder.CreateString(next.Subject)
		first := builder.CreateString(next.FirstName)
		last := builder.CreateString(next.LastName)
		username := builder.CreateString(next.Username)
		photo := builder.CreateString(next.PhotoMediaID)
		video := builder.CreateString(next.VideoMediaID)
		wire.ChatMemberDataStart(builder)
		wire.ChatMemberDataAddSubject(builder, subject)
		wire.ChatMemberDataAddFirstName(builder, first)
		wire.ChatMemberDataAddLastName(builder, last)
		wire.ChatMemberDataAddUsername(builder, username)
		wire.ChatMemberDataAddPhotoMediaId(builder, photo)
		wire.ChatMemberDataAddVideoMediaId(builder, video)
		wire.ChatMemberDataAddRole(builder, next.Role)
		offsets[index] = wire.ChatMemberDataEnd(builder)
	}

	wire.ChatMembersDataStartMembersVector(builder, len(offsets))

	for index := len(offsets) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(offsets[index])
	}

	vector := builder.EndVector(len(offsets))
	wire.ChatMembersDataStart(builder)
	wire.ChatMembersDataAddMembers(builder, vector)
	builder.Finish(wire.ChatMembersDataEnd(builder))

	return builder.FinishedBytes()
}

func DecodeChatMembersData(body []byte) ([]ChatMemberData, error) {
	var members []ChatMemberData

	err := safely(func() {
		table := wire.GetRootAsChatMembersData(body, 0)
		if table.MembersLength() > maxListItems {
			panic("too many members")
		}

		members = make([]ChatMemberData, table.MembersLength())

		var row wire.ChatMemberData

		for index := range members {
			if !table.Members(&row, index) {
				panic("member")
			}

			members[index] = ChatMemberData{
				Subject:      string(row.Subject()),
				FirstName:    string(row.FirstName()),
				LastName:     string(row.LastName()),
				Username:     string(row.Username()),
				PhotoMediaID: string(row.PhotoMediaId()),
				VideoMediaID: string(row.VideoMediaId()),
				Role:         row.Role(),
			}
		}
	})

	return members, err
}

func EncodeChatSearchData(chats []ChatData) []byte {
	builder := flatbuffers.NewBuilder(1024)
	offsets := make([]flatbuffers.UOffsetT, len(chats))

	for index, next := range chats {
		offsets[index] = buildChat(builder, next)
	}

	wire.ChatSearchDataStartChatsVector(builder, len(offsets))

	for index := len(offsets) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(offsets[index])
	}

	vector := builder.EndVector(len(offsets))
	wire.ChatSearchDataStart(builder)
	wire.ChatSearchDataAddChats(builder, vector)
	builder.Finish(wire.ChatSearchDataEnd(builder))

	return builder.FinishedBytes()
}

func EncodeChatsChanged() []byte {
	builder := flatbuffers.NewBuilder(16)
	wire.ChatsChangedStart(builder)
	builder.Finish(wire.ChatsChangedEnd(builder))

	return builder.FinishedBytes()
}
