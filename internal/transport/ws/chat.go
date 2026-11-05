package ws

import (
	"context"
	"errors"
	"log/slog"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/transport/codec"
)

type Messaging interface {
	OpenDirect(ctx context.Context, principal identity.Principal, peer string) (message.Chat, error)
	Send(ctx context.Context, principal identity.Principal, chatID, clientID, text string) (message.Message, error)
	History(
		ctx context.Context,
		principal identity.Principal,
		chatID string,
		before int64,
		limit int,
	) ([]message.Message, error)
	Chats(ctx context.Context, principal identity.Principal, before int64) ([]message.Chat, error)
}

func (h *Handler) chatOpenDirect(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	peer, err := codec.DecodeChatOpenDirect(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	chat, err := h.messaging.OpenDirect(ctx, principal, peer)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindChatData, codec.EncodeChatData(chatData(chat)))
}

func (h *Handler) messageSend(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	send, err := codec.DecodeMessageSend(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	stored, err := h.messaging.Send(ctx, principal, send.ChatID, send.ClientID, send.Text)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindMessageData, messageBody(stored))
}

func (h *Handler) historyGet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	get, err := codec.DecodeHistoryGet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	messages, err := h.messaging.History(ctx, principal, get.ChatID, get.BeforeID, int(get.Limit))
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	rows := make([]codec.MessageData, len(messages))
	for index, next := range messages {
		rows[index] = messageData(next)
	}

	return reply(request, codec.KindHistoryData, codec.EncodeHistoryData(rows))
}

func (h *Handler) chatsGet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	before, err := codec.DecodeChatsGet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	chats, err := h.messaging.Chats(ctx, principal, before)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	rows := make([]codec.ChatData, len(chats))
	for index, next := range chats {
		rows[index] = chatData(next)
	}

	return reply(request, codec.KindChatsData, codec.EncodeChatsData(rows))
}

func (h *Handler) chatFailure(ctx context.Context, request codec.Frame, err error) codec.Frame {
	switch {
	case errors.Is(err, identity.ErrForbidden):
		return failure(request, codeForbidden, "forbidden")
	case errors.Is(err, message.ErrChatNotFound):
		return failure(request, codeNotFound, "chat_not_found")
	case errors.Is(err, message.ErrPeerNotFound):
		return failure(request, codeNotFound, "peer_not_found")
	case errors.Is(err, message.ErrBlocked):
		return failure(request, codeForbidden, "blocked")
	case errors.Is(err, message.ErrRestricted):
		return failure(request, codeForbidden, "privacy_restricted")
	case errors.Is(err, message.ErrInvalidPeer):
		return failure(request, codeUnprocessable, "invalid_peer")
	case errors.Is(err, message.ErrInvalidText):
		return failure(request, codeUnprocessable, "invalid_text")
	case errors.Is(err, message.ErrInvalidClientID):
		return failure(request, codeUnprocessable, "invalid_client_id")
	default:
		h.logger.ErrorContext(ctx, "chat request failed", slog.Any("error", err))

		return failure(request, codeInternal, "internal")
	}
}

func messageData(next message.Message) codec.MessageData {
	return codec.MessageData{
		ChatID:       next.ChatID,
		MessageID:    next.ID,
		Sender:       next.Sender,
		ClientID:     next.ClientID,
		Text:         next.Text,
		SentAtUnixMs: next.SentAt.UnixMilli(),
	}
}

func messageBody(next message.Message) []byte {
	return codec.EncodeMessageData(messageData(next))
}

func chatData(chat message.Chat) codec.ChatData {
	data := codec.ChatData{
		ChatID:           chat.ID,
		PeerSubject:      chat.Peer.Subject,
		PeerFirstName:    chat.Peer.FirstName,
		PeerLastName:     chat.Peer.LastName,
		PeerUsername:     chat.Peer.Username,
		PeerPhotoMediaID: chat.Peer.PhotoMediaID,
		PeerVideoMediaID: chat.Peer.VideoMediaID,
	}

	if chat.Last != nil {
		last := messageData(*chat.Last)
		data.Last = &last
	}

	return data
}
