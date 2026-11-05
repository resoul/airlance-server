package ws_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/transport/codec"
	"go.airlance.org/server/internal/transport/ws"
)

type fakeMessaging struct {
	ws.Messaging
}

func (fakeMessaging) OpenDirect(_ context.Context, _ identity.Principal, peer string) (message.Chat, error) {
	switch peer {
	case "":
		return message.Chat{}, message.ErrInvalidPeer
	case "missing":
		return message.Chat{}, message.ErrPeerNotFound
	case "denied":
		return message.Chat{}, identity.ErrForbidden
	}

	return message.Chat{ID: "c", Peer: message.Peer{Subject: peer, FirstName: "Петро"}}, nil
}

func (fakeMessaging) Send(
	_ context.Context,
	principal identity.Principal,
	chatID, clientID, text string,
) (message.Message, error) {
	switch {
	case chatID == "foreign":
		return message.Message{}, message.ErrChatNotFound
	case text == "":
		return message.Message{}, message.ErrInvalidText
	case clientID == "":
		return message.Message{}, message.ErrInvalidClientID
	case text == "boom":
		return message.Message{}, errors.New("secret database detail")
	}

	return message.Message{
		ChatID:   chatID,
		ID:       7,
		Sender:   principal.Subject,
		ClientID: clientID,
		Text:     text,
		SentAt:   time.UnixMilli(1_800_000_000_123),
	}, nil
}

func (f fakeMessaging) SendContent(
	ctx context.Context,
	principal identity.Principal,
	chatID, clientID string,
	outgoing message.Outgoing,
) (message.Message, error) {
	return f.Send(ctx, principal, chatID, clientID, outgoing.Text)
}

func (fakeMessaging) History(
	_ context.Context,
	_ identity.Principal,
	chatID string,
	before int64,
	_ int,
) ([]message.Message, error) {
	if chatID == "foreign" {
		return nil, message.ErrChatNotFound
	}

	return []message.Message{{ChatID: chatID, ID: before - 1, Text: "старое", SentAt: time.UnixMilli(5)}}, nil
}

func (fakeMessaging) Chats(context.Context, identity.Principal, int64) ([]message.Chat, error) {
	last := message.Message{ChatID: "c", ID: 7, Text: "привет", SentAt: time.UnixMilli(5)}

	return []message.Chat{{ID: "c", Peer: message.Peer{Subject: "petro"}, Last: &last}}, nil
}

func sendFrame(id uint64, chatID, clientID, text string) codec.Frame {
	body := codec.EncodeMessageSend(codec.MessageSend{ChatID: chatID, ClientID: clientID, Text: text})

	return codec.Frame{RequestID: id, Kind: codec.KindMessageSend, Body: body}
}

func TestAChatCanBeOpenedAndWrittenIn(t *testing.T) {
	t.Parallel()

	c := signedIn(t, start(t, time.Minute, ws.DefaultLimits()))

	open := codec.Frame{RequestID: 2, Kind: codec.KindChatOpenDirect, Body: codec.EncodeChatOpenDirect("petro")}
	response := c.ask(open)
	chat, err := codec.DecodeChatData(response.Body)
	if response.Kind != codec.KindChatData || err != nil || chat.ChatID != "c" || chat.PeerFirstName != "Петро" {
		t.Fatalf("got %+v, %+v, %v", response, chat, err)
	}

	sent := c.ask(sendFrame(3, "c", "local", "привет"))
	stored, err := codec.DecodeMessageData(sent.Body)
	if sent.Kind != codec.KindMessageData || sent.Seq != 0 || err != nil {
		t.Fatalf("got %+v, %v", sent, err)
	}

	if stored.MessageID != 7 || stored.Sender != "anna" || stored.ClientID != "local" || stored.SentAtUnixMs != 1_800_000_000_123 {
		t.Fatalf("got %+v", stored)
	}
}

func TestHistoryAndChatsAreRead(t *testing.T) {
	t.Parallel()

	c := signedIn(t, start(t, time.Minute, ws.DefaultLimits()))

	get := codec.HistoryGet{ChatID: "c", BeforeID: 10, Limit: 20}
	response := c.ask(codec.Frame{RequestID: 2, Kind: codec.KindHistoryGet, Body: codec.EncodeHistoryGet(get)})
	messages, err := codec.DecodeHistoryData(response.Body)
	if response.Kind != codec.KindHistoryData || err != nil || len(messages) != 1 || messages[0].MessageID != 9 {
		t.Fatalf("got %+v, %+v, %v", response, messages, err)
	}

	response = c.ask(codec.Frame{RequestID: 3, Kind: codec.KindChatsGet, Body: codec.EncodeChatsGet(0)})
	chats, err := codec.DecodeChatsData(response.Body)
	if response.Kind != codec.KindChatsData || err != nil || len(chats) != 1 || chats[0].Last == nil {
		t.Fatalf("got %+v, %+v, %v", response, chats, err)
	}
}

func TestChatFailuresGiveOnlyACode(t *testing.T) {
	t.Parallel()

	c := signedIn(t, start(t, time.Minute, ws.DefaultLimits()))
	open := func(id uint64, peer string) codec.Frame {
		return codec.Frame{RequestID: id, Kind: codec.KindChatOpenDirect, Body: codec.EncodeChatOpenDirect(peer)}
	}

	assertError(t, c.ask(open(2, "")), 422, "invalid_peer")
	assertError(t, c.ask(open(3, "missing")), 404, "peer_not_found")
	assertError(t, c.ask(open(4, "denied")), 403, "forbidden")
	assertError(t, c.ask(sendFrame(5, "foreign", "l", "hi")), 404, "chat_not_found")
	assertError(t, c.ask(sendFrame(6, "c", "l", "")), 422, "invalid_text")
	assertError(t, c.ask(sendFrame(7, "c", "", "hi")), 422, "invalid_client_id")
	assertError(t, c.ask(sendFrame(8, "c", "l", "boom")), 500, "internal")

	get := codec.HistoryGet{ChatID: "foreign"}
	assertError(t, c.ask(codec.Frame{RequestID: 9, Kind: codec.KindHistoryGet, Body: codec.EncodeHistoryGet(get)}), 404, "chat_not_found")

	broken := codec.Frame{RequestID: 10, Kind: codec.KindMessageSend, Body: []byte{1}}
	assertError(t, c.ask(broken), 400, "invalid body")
}

func TestAMessageArrivesAsAnUpdate(t *testing.T) {
	t.Parallel()

	target := start(t, time.Minute, ws.DefaultLimits())
	c := signedIn(t, target)
	c.ask(subscribeFrame(2, 0))

	target.updates.publishMessage(message.Message{
		ChatID: "c",
		ID:     7,
		Sender: "petro",
		Text:   "привет",
		SentAt: time.UnixMilli(5),
	})

	frame := c.next()
	if frame.Seq != 1 || frame.RequestID != 0 || frame.Kind != codec.KindMessageData {
		t.Fatalf("got %+v", frame)
	}

	data, err := codec.DecodeMessageData(frame.Body)
	if err != nil || data.Text != "привет" || data.Sender != "petro" {
		t.Fatalf("got %+v, %v", data, err)
	}
}
