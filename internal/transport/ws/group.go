package ws

import (
	"context"
	"errors"

	"go.airlance.org/server/internal/domain/directory"
	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/transport/codec"
	usecase "go.airlance.org/server/internal/usecase/message"
)

func (h *Handler) chatCreate(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	create, err := codec.DecodeChatCreate(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	chat, err := h.chatList.Create(ctx, principal, usecase.NewChat{
		ClientID: create.ClientID,
		Kind:     message.Kind(create.Kind),
		Title:    create.Title,
		About:    create.About,
		Link:     create.Link,
		People:   create.Members,
	})
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindChatData, codec.EncodeChatData(chatData(chat)))
}

func (h *Handler) chatJoin(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	code, err := codec.DecodeChatJoin(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	chat, err := h.chatList.Join(ctx, principal, code)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindChatData, codec.EncodeChatData(chatData(chat)))
}

func (h *Handler) chatLeave(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	id, err := codec.DecodeChatLeave(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	if err := h.chatList.Leave(ctx, principal, id); err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindChatsChanged, codec.EncodeChatsChanged())
}

func (h *Handler) chatMembersGet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	id, err := codec.DecodeChatMembersGet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	members, err := h.admin.Members(ctx, principal, id)

	return h.membersReply(ctx, request, members, err)
}

func (h *Handler) chatMemberAdd(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	change, err := codec.DecodeChatMemberAdd(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	members, err := h.admin.AddMember(
		ctx, principal, change.ChatID, change.Subject, message.Role(change.Role), message.Rights(change.Rights),
	)

	return h.membersReply(ctx, request, members, err)
}

func (h *Handler) chatMemberRemove(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	change, err := codec.DecodeChatMemberRemove(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	members, err := h.admin.RemoveMember(ctx, principal, change.ChatID, change.Subject, change.Ban)

	return h.membersReply(ctx, request, members, err)
}

func (h *Handler) membersReply(
	ctx context.Context,
	request codec.Frame,
	members []message.Member,
	err error,
) codec.Frame {
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	rows := make([]codec.ChatMemberData, len(members))
	for index, next := range members {
		rows[index] = codec.ChatMemberData{
			Subject:      next.Peer.Subject,
			FirstName:    next.Peer.FirstName,
			LastName:     next.Peer.LastName,
			Username:     next.Peer.Username,
			PhotoMediaID: next.Peer.PhotoMediaID,
			VideoMediaID: next.Peer.VideoMediaID,
			Role:         int32(next.Role),
			Rights:       int64(next.Rights),
		}
	}

	return reply(request, codec.KindChatMembersData, codec.EncodeChatMembersData(rows))
}

func (h *Handler) chatPinSet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	id, isPinned, err := codec.DecodeChatPinSet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	chat, err := h.chatList.SetPinned(ctx, principal, id, isPinned)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindChatData, codec.EncodeChatData(chatData(chat)))
}

func (h *Handler) chatMuteSet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	id, isMuted, err := codec.DecodeChatMuteSet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	chat, err := h.chatList.SetMuted(ctx, principal, id, isMuted)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindChatData, codec.EncodeChatData(chatData(chat)))
}

func (h *Handler) chatResolve(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	code, err := codec.DecodeChatResolve(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	chat, err := h.directory.ResolveChat(ctx, principal, code)
	if errors.Is(err, directory.ErrTooManySearches) {
		return h.searchFailure(ctx, request, err)
	}

	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindChatData, codec.EncodeChatData(chatData(chat)))
}

func (h *Handler) chatInviteReset(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	id, err := codec.DecodeChatInviteReset(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	chat, err := h.chatList.ResetInvite(ctx, principal, id)
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindChatData, codec.EncodeChatData(chatData(chat)))
}

func (h *Handler) userResolve(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	username, err := codec.DecodeUserResolve(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	person, err := h.directory.ResolveUser(ctx, principal, username)
	if errors.Is(err, directory.ErrNotFound) {
		return failure(request, codeNotFound, "peer_not_found")
	}

	if err != nil {
		return h.searchFailure(ctx, request, err)
	}

	return reply(request, codec.KindSearchResults, codec.EncodeSearchResults([]codec.SearchPerson{{
		Subject:      person.Subject,
		FirstName:    person.FirstName,
		LastName:     person.LastName,
		Username:     person.Username,
		PhotoMediaID: person.PhotoMediaID,
		VideoMediaID: person.VideoMediaID,
	}}))
}

func (h *Handler) chatSearch(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	query, err := codec.DecodeChatSearch(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	chats, err := h.directory.SearchChats(ctx, principal, query)
	if err != nil {
		return h.searchFailure(ctx, request, err)
	}

	rows := make([]codec.ChatData, len(chats))
	for index, next := range chats {
		rows[index] = chatData(next)
	}

	return reply(request, codec.KindChatSearchData, codec.EncodeChatSearchData(rows))
}

func (h *Handler) chatMemberUnban(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	change, err := codec.DecodeChatMemberUnban(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	members, err := h.admin.Unban(ctx, principal, change.ChatID, change.Subject)

	return h.membersReply(ctx, request, members, err)
}

func (h *Handler) chatBansGet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	id, err := codec.DecodeChatBansGet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	members, err := h.admin.Bans(ctx, principal, id)

	return h.membersReply(ctx, request, members, err)
}

func (h *Handler) chatRightsSet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	id, rights, err := codec.DecodeChatRightsSet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	chat, err := h.admin.SetMemberRights(ctx, principal, id, message.Rights(rights))
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindChatData, codec.EncodeChatData(chatData(chat)))
}

func (h *Handler) chatInfoSet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	info, err := codec.DecodeChatInfoSet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	chat, err := h.admin.SetInfo(ctx, principal, info.ChatID, message.ChatInfo{
		Title: info.Title, About: info.About, Link: info.Link,
	})
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindChatData, codec.EncodeChatData(chatData(chat)))
}

func (h *Handler) chatRulesSet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	id, rules, err := codec.DecodeChatRulesSet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	chat, err := h.admin.SetRules(ctx, principal, id, message.ChatRules{
		NoCopy: rules.NoCopy, Signs: rules.Signs, ReactionsMode: rules.ReactionsMode,
		Reactions: rules.Reactions, PerPost: rules.PerPost,
	})
	if err != nil {
		return h.chatFailure(ctx, request, err)
	}

	return reply(request, codec.KindChatData, codec.EncodeChatData(chatData(chat)))
}
