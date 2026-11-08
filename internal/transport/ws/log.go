package ws

import (
	"context"
	"strings"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/transport/codec"
)

func (h *Handler) chatLogGet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	id, err := codec.DecodeChatLogGet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	entries, err := h.admin.ChatLog(ctx, principal, id)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	rows := make([]codec.ChatLogEntry, len(entries))
	for index, next := range entries {
		rows[index] = codec.ChatLogEntry{
			ID:           next.ID,
			ActorSubject: next.Actor.Subject,
			ActorName:    displayName(next.Actor),
			Kind:         int32(next.Kind),
			Detail:       next.Detail,
			AtUnixMs:     next.At.UnixMilli(),
		}
	}

	return reply(request, codec.KindChatLogData, codec.EncodeChatLogData(rows))
}

func displayName(peer message.Peer) string {
	name := strings.TrimSpace(peer.FirstName + " " + peer.LastName)
	if name == "" {
		return peer.Username
	}

	return name
}
