package ws

import (
	"context"
	"errors"
	"log/slog"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/transport/codec"
	usecase "go.airlance.org/server/internal/usecase/message"
)

type Messaging interface {
	OpenDirect(ctx context.Context, principal identity.Principal, peer string) (message.Chat, error)
	Send(ctx context.Context, principal identity.Principal, chatID, clientID, text string) (message.Message, error)
	SendContent(
		ctx context.Context,
		principal identity.Principal,
		chatID, clientID string,
		outgoing message.Outgoing,
	) (message.Message, error)
	History(
		ctx context.Context,
		principal identity.Principal,
		chatID string,
		before int64,
		limit int,
	) ([]message.Message, error)
	Chats(ctx context.Context, principal identity.Principal, before int64) ([]message.Chat, error)
	Create(ctx context.Context, principal identity.Principal, next usecase.NewChat) (message.Chat, error)
	Join(ctx context.Context, principal identity.Principal, code string) (message.Chat, error)
	Leave(ctx context.Context, principal identity.Principal, chatID string) error
	Members(ctx context.Context, principal identity.Principal, chatID string) ([]message.Member, error)
	ResetInvite(ctx context.Context, principal identity.Principal, chatID string) (message.Chat, error)
	SetPinned(ctx context.Context, principal identity.Principal, chatID string, isPinned bool) (message.Chat, error)
	AddMember(
		ctx context.Context,
		principal identity.Principal,
		chatID, subject string,
		role message.Role,
	) ([]message.Member, error)
	RemoveMember(
		ctx context.Context,
		principal identity.Principal,
		chatID, subject string,
	) ([]message.Member, error)
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

	outgoing := message.Outgoing{Text: send.Text, ForwardName: send.ForwardName}
	for _, next := range send.Attachments {
		outgoing.Attachments = append(outgoing.Attachments, message.Attachment{
			Kind:         message.AttachmentKind(next.Kind),
			MediaID:      next.MediaID,
			ThumbMediaID: next.ThumbMediaID,
			Name:         next.Name,
			Size:         next.Size,
			Width:        next.Width,
			Height:       next.Height,
			DurationMs:   next.DurationMs,
			Waveform:     next.Waveform,
			Title:        next.Title,
			Performer:    next.Performer,
		})
	}

	stored, err := h.messaging.SendContent(ctx, principal, send.ChatID, send.ClientID, outgoing)
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
	case errors.Is(err, message.ErrNotAllowed):
		return failure(request, codeForbidden, "not_allowed")
	case errors.Is(err, message.ErrTooManyPinned):
		return failure(request, codeConflict, "too_many_pinned")
	case errors.Is(err, message.ErrLinkTaken):
		return failure(request, codeConflict, "link_taken")
	case errors.Is(err, message.ErrInvalidTitle):
		return failure(request, codeUnprocessable, "invalid_title")
	case errors.Is(err, message.ErrInvalidAbout):
		return failure(request, codeUnprocessable, "invalid_about")
	case errors.Is(err, message.ErrInvalidLink):
		return failure(request, codeUnprocessable, "invalid_link")
	case errors.Is(err, message.ErrInvalidKind):
		return failure(request, codeUnprocessable, "invalid_kind")
	case errors.Is(err, message.ErrInvalidPeer):
		return failure(request, codeUnprocessable, "invalid_peer")
	case errors.Is(err, message.ErrInvalidAttachment):
		return failure(request, codeUnprocessable, "invalid_attachment")
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
	attachments := make([]codec.Attachment, len(next.Attachments))
	for index, item := range next.Attachments {
		attachments[index] = codec.Attachment{
			Kind:         int32(item.Kind),
			MediaID:      item.MediaID,
			ThumbMediaID: item.ThumbMediaID,
			Name:         item.Name,
			Size:         item.Size,
			Width:        item.Width,
			Height:       item.Height,
			DurationMs:   item.DurationMs,
			Waveform:     item.Waveform,
			Title:        item.Title,
			Performer:    item.Performer,
		}
	}

	return codec.MessageData{
		ForwardName:  next.ForwardName,
		Attachments:  attachments,
		ChatID:       next.ChatID,
		MessageID:    next.ID,
		Sender:       next.Sender,
		ClientID:     next.ClientID,
		Text:         next.Text,
		SentAtUnixMs: next.SentAt.UnixMilli(),
		FirstName:    next.Author.FirstName,
		LastName:     next.Author.LastName,
		PhotoMediaID: next.Author.PhotoMediaID,
		VideoMediaID: next.Author.VideoMediaID,
		AgentName:    next.AgentName,
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
		Kind:             int32(chat.Kind),
		Title:            chat.Title,
		About:            chat.About,
		Link:             chat.Link,
		InviteToken:      chat.InviteToken,
		MemberCount:      int32(chat.MemberCount),
		Role:             int32(chat.Role),
		IsMember:         chat.IsMember,
		IsPinned:         chat.IsPinned,
	}

	if chat.Last != nil {
		last := messageData(*chat.Last)
		data.Last = &last
	}

	return data
}
