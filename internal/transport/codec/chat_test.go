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
