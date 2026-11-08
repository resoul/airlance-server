package codec_test

import (
	"math/rand/v2"
	"reflect"
	"testing"

	"go.airlance.org/server/internal/transport/codec"
)

func TestChatRequestsRoundTrip(t *testing.T) {
	t.Parallel()

	peer, err := codec.DecodeChatOpenDirect(codec.EncodeChatOpenDirect("petro"))
	if err != nil || peer != "petro" {
		t.Fatalf("got %q, %v", peer, err)
	}

	send := codec.MessageSend{ChatID: "c", ClientID: "local", Text: "привет"}

	gotSend, err := codec.DecodeMessageSend(codec.EncodeMessageSend(send))
	if err != nil || !reflect.DeepEqual(gotSend, send) {
		t.Fatalf("got %+v, %v", gotSend, err)
	}

	get := codec.HistoryGet{ChatID: "c", BeforeID: 7, Limit: 20}

	gotGet, err := codec.DecodeHistoryGet(codec.EncodeHistoryGet(get))
	if err != nil || gotGet != get {
		t.Fatalf("got %+v, %v", gotGet, err)
	}

	before, err := codec.DecodeChatsGet(codec.EncodeChatsGet(9))
	if err != nil || before != 9 {
		t.Fatalf("got %d, %v", before, err)
	}
}

func TestChatRepliesRoundTrip(t *testing.T) {
	t.Parallel()

	message := codec.MessageData{
		ChatID:       "c",
		MessageID:    7,
		Sender:       "anna",
		ClientID:     "local",
		Text:         "привет",
		SentAtUnixMs: 1_800_000_000_123,
	}

	got, err := codec.DecodeMessageData(codec.EncodeMessageData(message))
	if err != nil || !reflect.DeepEqual(got, message) {
		t.Fatalf("got %+v, %v", got, err)
	}

	history, err := codec.DecodeHistoryData(codec.EncodeHistoryData([]codec.MessageData{message, {ChatID: "c", MessageID: 6}}))
	if err != nil || len(history) != 2 || !reflect.DeepEqual(history[0], message) || history[1].MessageID != 6 {
		t.Fatalf("got %+v, %v", history, err)
	}

	empty, err := codec.DecodeHistoryData(codec.EncodeHistoryData(nil))
	if err != nil || len(empty) != 0 {
		t.Fatalf("got %+v, %v", empty, err)
	}

	withLast := codec.ChatData{ChatID: "c", PeerSubject: "p", PeerFirstName: "Петро", PeerUsername: "petro", Last: &message}

	chat, err := codec.DecodeChatData(codec.EncodeChatData(withLast))
	if err != nil || chat.PeerFirstName != "Петро" || chat.Last == nil || !reflect.DeepEqual(*chat.Last, message) {
		t.Fatalf("got %+v, %v", chat, err)
	}

	noLast, err := codec.DecodeChatData(codec.EncodeChatData(codec.ChatData{ChatID: "c"}))
	if err != nil || noLast.Last != nil {
		t.Fatalf("got %+v, %v", noLast, err)
	}

	chats, err := codec.DecodeChatsData(codec.EncodeChatsData([]codec.ChatData{withLast, {ChatID: "d"}}))
	if err != nil || len(chats) != 2 || chats[0].Last == nil || chats[1].ChatID != "d" || chats[1].Last != nil {
		t.Fatalf("got %+v, %v", chats, err)
	}
}

func TestDamagedChatBodiesNeverPanic(t *testing.T) {
	t.Parallel()

	message := codec.MessageData{ChatID: "c", MessageID: 7, Sender: "anna", ClientID: "local", Text: "привет"}
	good := [][]byte{
		codec.EncodeMessageSend(codec.MessageSend{ChatID: "c", ClientID: "l", Text: "привет"}),
		codec.EncodeHistoryGet(codec.HistoryGet{ChatID: "c", BeforeID: 7, Limit: 20}),
		codec.EncodeHistoryData([]codec.MessageData{message, message}),
		codec.EncodeChatsData([]codec.ChatData{{ChatID: "c", Last: &message}}),
	}
	random := rand.New(rand.NewPCG(3, 4))

	for range 20_000 {
		damaged := append([]byte(nil), good[random.IntN(len(good))]...)
		for range 1 + random.IntN(8) {
			damaged[random.IntN(len(damaged))] = byte(random.IntN(256))
		}

		_, _ = codec.DecodeChatOpenDirect(damaged)
		_, _ = codec.DecodeMessageSend(damaged)
		_, _ = codec.DecodeHistoryGet(damaged)
		_, _ = codec.DecodeChatsGet(damaged)
		_, _ = codec.DecodeMessageData(damaged)
		_, _ = codec.DecodeHistoryData(damaged)
		_, _ = codec.DecodeChatData(damaged)
		_, _ = codec.DecodeChatsData(damaged)
	}
}

func TestAttachmentsSurviveTheWire(t *testing.T) {
	t.Parallel()

	send := codec.MessageSend{ChatID: "c", ClientID: "l", ForwardName: "Канал", Attachments: []codec.Attachment{{
		Kind: 5, MediaID: "m", ThumbMediaID: "t", Name: "song.mp3", Size: 9, Width: 1, Height: 2, DurationMs: 3,
		Waveform: []byte{4, 5}, Title: "Песня", Performer: "Группа",
	}}}

	got, err := codec.DecodeMessageSend(codec.EncodeMessageSend(send))
	if err != nil || !reflect.DeepEqual(got, send) {
		t.Fatalf("got %+v, %v", got, err)
	}

	data := codec.MessageData{ChatID: "c", ForwardName: "Канал", Attachments: send.Attachments}

	back, err := codec.DecodeMessageData(codec.EncodeMessageData(data))
	if err != nil || !reflect.DeepEqual(back, data) {
		t.Fatalf("got %+v, %v", back, err)
	}
}

func TestMessageEditingRoundTrip(t *testing.T) {
	t.Parallel()

	entities := []codec.Entity{{Kind: 3, Offset: 1, Length: 4}, {Kind: 2, Offset: 0, Length: 3, UserID: "petro"}}

	send := codec.MessageSend{ChatID: "c", ClientID: "l", Text: "hi", ReplyToID: 12, Entities: entities}

	gotSend, err := codec.DecodeMessageSend(codec.EncodeMessageSend(send))
	if err != nil || !reflect.DeepEqual(gotSend, send) {
		t.Fatalf("got %+v, %v", gotSend, err)
	}

	edit := codec.MessageEdit{ChatID: "c", MessageID: 7, Text: "new", Entities: entities}

	gotEdit, err := codec.DecodeMessageEdit(codec.EncodeMessageEdit(edit))
	if err != nil || !reflect.DeepEqual(gotEdit, edit) {
		t.Fatalf("got %+v, %v", gotEdit, err)
	}

	remove := codec.MessageDelete{ChatID: "c", MessageIDs: []int64{3, 4, 5}, ForEveryone: true}

	gotRemove, err := codec.DecodeMessageDelete(codec.EncodeMessageDelete(remove))
	if err != nil || !reflect.DeepEqual(gotRemove, remove) {
		t.Fatalf("got %+v, %v", gotRemove, err)
	}

	chatID, ids, err := codec.DecodeMessagesDeleted(codec.EncodeMessagesDeleted("c", []int64{9}))
	if err != nil || chatID != "c" || !reflect.DeepEqual(ids, []int64{9}) {
		t.Fatalf("got %q %v, %v", chatID, ids, err)
	}

	data := codec.MessageData{ChatID: "c", MessageID: 7, Text: "x", ReplyToID: 2, Entities: entities, EditedAtMs: 55}

	gotData, err := codec.DecodeMessageData(codec.EncodeMessageData(data))
	if err != nil || !reflect.DeepEqual(gotData, data) {
		t.Fatalf("got %+v, %v", gotData, err)
	}
}

func TestPinsAndStatsRoundTrip(t *testing.T) {
	t.Parallel()

	set := codec.MessagePinSet{ChatID: "c", MessageID: 4, IsPinned: true}

	gotSet, err := codec.DecodeMessagePinSet(codec.EncodeMessagePinSet(set))
	if err != nil || gotSet != set {
		t.Fatalf("got %+v, %v", gotSet, err)
	}

	chatID, ids, err := codec.DecodePinsData(codec.EncodePinsData("c", []int64{1, 2}))
	if err != nil || chatID != "c" || !reflect.DeepEqual(ids, []int64{1, 2}) {
		t.Fatalf("got %q %v, %v", chatID, ids, err)
	}

	get := codec.PostStatsGet{ChatID: "c", MessageIDs: []int64{5, 6}, MarkViewed: true}

	gotGet, err := codec.DecodePostStatsGet(codec.EncodePostStatsGet(get))
	if err != nil || !reflect.DeepEqual(gotGet, get) {
		t.Fatalf("got %+v, %v", gotGet, err)
	}

	items := []codec.PostStat{{MessageID: 5, Views: 9, Comments: 3, Recent: []codec.PostCommenter{{Subject: "p", FirstName: "Петро"}}}}

	gotChat, gotItems, err := codec.DecodePostStatsData(codec.EncodePostStatsData("c", items))
	if err != nil || gotChat != "c" || !reflect.DeepEqual(gotItems, items) {
		t.Fatalf("got %q %+v, %v", gotChat, gotItems, err)
	}

	send := codec.MessageSend{ChatID: "c", ClientID: "l", Text: "t", ThreadID: 8}

	gotSend, err := codec.DecodeMessageSend(codec.EncodeMessageSend(send))
	if err != nil || gotSend.ThreadID != 8 {
		t.Fatalf("got %+v, %v", gotSend, err)
	}
}

func TestReadAndReactionsRoundTrip(t *testing.T) {
	t.Parallel()

	chatID, upTo, err := codec.DecodeChatReadSet(codec.EncodeChatReadSet("c", 42))
	if err != nil || chatID != "c" || upTo != 42 {
		t.Fatalf("got %q %d, %v", chatID, upTo, err)
	}

	read := codec.ReadData{ChatID: "c", MyReadID: 5, PeerReadID: 4, Unread: 3}

	gotRead, err := codec.DecodeReadData(codec.EncodeReadData(read))
	if err != nil || gotRead != read {
		t.Fatalf("got %+v, %v", gotRead, err)
	}

	set := codec.ReactionSet{ChatID: "c", MessageID: 9, Emoji: "🔥"}

	gotSet, err := codec.DecodeReactionSet(codec.EncodeReactionSet(set))
	if err != nil || gotSet != set {
		t.Fatalf("got %+v, %v", gotSet, err)
	}

	data := codec.MessageData{
		ChatID: "c", MessageID: 9, Text: "x", MyReaction: "🔥",
		Reactions: []codec.ReactionCount{{Emoji: "🔥", Count: 2}, {Emoji: "👍", Count: 1}},
	}

	gotData, err := codec.DecodeMessageData(codec.EncodeMessageData(data))
	if err != nil || !reflect.DeepEqual(gotData, data) {
		t.Fatalf("got %+v, %v", gotData, err)
	}

	chat := codec.ChatData{ChatID: "c", MyReadID: 7, PeerReadID: 6, Unread: 2}

	gotChats, err := codec.DecodeChatsData(codec.EncodeChatsData([]codec.ChatData{chat}))
	if err != nil || gotChats[0].MyReadID != 7 || gotChats[0].PeerReadID != 6 || gotChats[0].Unread != 2 {
		t.Fatalf("got %+v, %v", gotChats, err)
	}
}

func TestCardAttachmentsRoundTrip(t *testing.T) {
	t.Parallel()

	send := codec.MessageSend{ChatID: "c", ClientID: "l", Attachments: []codec.Attachment{
		{Kind: 7, Latitude: 50.4501, Longitude: 30.5234, Title: "Київ", Name: "Хрещатик"},
		{Kind: 8, Name: "Олена", Phone: "+380501112233"},
	}}

	got, err := codec.DecodeMessageSend(codec.EncodeMessageSend(send))
	if err != nil || !reflect.DeepEqual(got, send) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestPollRoundTrip(t *testing.T) {
	t.Parallel()

	poll := &codec.Poll{
		Question: "Куди?", Anonymous: true, IsQuiz: true, CorrectOption: 1, Explanation: "бо море", CloseAtMs: 99,
		TotalVoters: 3, MyChoices: []int32{1}, IsClosed: true,
		Options: []codec.PollOption{
			{Text: "Ліс", Votes: 1},
			{Text: "Море", Votes: 2, Voters: []codec.PostCommenter{{Subject: "p", FirstName: "Петро"}}},
		},
	}

	send := codec.MessageSend{ChatID: "c", ClientID: "l", Poll: poll}

	gotSend, err := codec.DecodeMessageSend(codec.EncodeMessageSend(send))
	if err != nil || !reflect.DeepEqual(gotSend.Poll, poll) {
		t.Fatalf("got %+v, %v", gotSend.Poll, err)
	}

	data := codec.MessageData{ChatID: "c", MessageID: 4, Poll: poll}

	gotData, err := codec.DecodeMessageData(codec.EncodeMessageData(data))
	if err != nil || !reflect.DeepEqual(gotData.Poll, poll) {
		t.Fatalf("got %+v, %v", gotData.Poll, err)
	}

	vote := codec.PollVote{ChatID: "c", MessageID: 4, Options: []int32{0, 2}}

	gotVote, err := codec.DecodePollVote(codec.EncodePollVote(vote))
	if err != nil || !reflect.DeepEqual(gotVote, vote) {
		t.Fatalf("got %+v, %v", gotVote, err)
	}

	chatID, id, err := codec.DecodePollClose(codec.EncodePollClose("c", 4))
	if err != nil || chatID != "c" || id != 4 {
		t.Fatalf("got %q %d, %v", chatID, id, err)
	}
}

func TestArchiveAndDraftRoundTrip(t *testing.T) {
	t.Parallel()

	chatID, isArchived, err := codec.DecodeChatArchiveSet(codec.EncodeChatArchiveSet("c", true))
	if err != nil || chatID != "c" || !isArchived {
		t.Fatalf("got %q %v, %v", chatID, isArchived, err)
	}

	id, text, err := codec.DecodeDraftSet(codec.EncodeDraftSet("c", "чернетка"))
	if err != nil || id != "c" || text != "чернетка" {
		t.Fatalf("got %q %q, %v", id, text, err)
	}

	draft := codec.DraftData{ChatID: "c", Text: "x", UpdatedAtMs: 77}

	got, err := codec.DecodeDraftData(codec.EncodeDraftData(draft))
	if err != nil || got != draft {
		t.Fatalf("got %+v, %v", got, err)
	}

	chat := codec.ChatData{ChatID: "c", IsArchived: true, Draft: "ще", DraftAtMs: 5}

	chats, err := codec.DecodeChatsData(codec.EncodeChatsData([]codec.ChatData{chat}))
	if err != nil || !chats[0].IsArchived || chats[0].Draft != "ще" || chats[0].DraftAtMs != 5 {
		t.Fatalf("got %+v, %v", chats, err)
	}
}

func TestSettingsAndWallpapersRoundTrip(t *testing.T) {
	t.Parallel()

	key, err := codec.DecodeSettingsGet(codec.EncodeSettingsGet("appearance"))
	if err != nil || key != "appearance" {
		t.Fatalf("got %q, %v", key, err)
	}

	gotKey, data, err := codec.DecodeSettingsPut(codec.EncodeSettingsPut("appearance", []byte(`{"a":1}`)))
	if err != nil || gotKey != "appearance" || string(data) != `{"a":1}` {
		t.Fatalf("got %q %q, %v", gotKey, data, err)
	}

	next := codec.SettingsData{Key: "appearance", Version: 4, Data: []byte("x")}

	got, err := codec.DecodeSettingsData(codec.EncodeSettingsData(next))
	if err != nil || !reflect.DeepEqual(got, next) {
		t.Fatalf("got %+v, %v", got, err)
	}

	items := []codec.Wallpaper{{ID: "a", Name: "Ліс", MediaID: "m1"}, {ID: "b", Name: "Море", MediaID: "m2"}}

	gotItems, err := codec.DecodeWallpapers(codec.EncodeWallpapers(items))
	if err != nil || !reflect.DeepEqual(gotItems, items) {
		t.Fatalf("got %+v, %v", gotItems, err)
	}
}
