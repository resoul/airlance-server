package ws

import (
	"context"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/transport/codec"
)

type route func(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame

func (h *Handler) routes() map[codec.Kind]route {
	table := map[codec.Kind]route{}
	add := func(next route, kinds ...codec.Kind) {
		for _, kind := range kinds {
			table[kind] = next
		}
	}

	add(h.ping, codec.KindPing)
	add(h.profileGet, codec.KindProfileGet)
	add(h.profileSet, codec.KindProfileSet)
	add(h.profilePhotoAdd, codec.KindProfilePhotoAdd)
	add(h.profilePhotoRemove, codec.KindProfilePhotoRemove)
	add(h.publicProfileGet, codec.KindPublicProfileGet)
	add(h.mediaUpload, codec.KindMediaUploadRequest)
	add(h.mediaDownload, codec.KindMediaDownloadRequest)
	add(h.search, codec.KindSearch)
	add(h.contactsRequest, codec.KindContactsGet, codec.KindContactAdd, codec.KindContactAddPhone, codec.KindContactRemove, codec.KindContactsSync)
	add(h.privacyGet, codec.KindPrivacyGet)
	add(h.privacySet, codec.KindPrivacySet)
	add(h.blocksGet, codec.KindBlocksGet)
	add(h.blockSet, codec.KindBlockSet)
	add(h.storyPublish, codec.KindStoryPublish)
	add(h.storiesGet, codec.KindStoriesGet)
	add(h.storyView, codec.KindStoryView)
	add(h.storyDelete, codec.KindStoryDelete)
	add(h.chatOpenDirect, codec.KindChatOpenDirect)
	add(h.messageSend, codec.KindMessageSend)
	add(h.messageEdit, codec.KindMessageEdit)
	add(h.messageDelete, codec.KindMessageDelete)
	add(h.chatReadSet, codec.KindChatReadSet)
	add(h.chatMemberUnban, codec.KindChatMemberUnban)
	add(h.chatBansGet, codec.KindChatBansGet)
	add(h.chatInfoSet, codec.KindChatInfoSet)
	add(h.chatRulesSet, codec.KindChatRulesSet)
	add(h.chatLogGet, codec.KindChatLogGet)
	add(h.linkPreviewGet, codec.KindLinkPreviewGet)
	add(h.chatReadersGet, codec.KindChatReadersGet)
	add(h.typingSet, codec.KindTypingSet)
	add(h.chatRightsSet, codec.KindChatRightsSet)
	add(h.chatDeliveredSet, codec.KindChatDeliveredSet)
	add(h.pollVote, codec.KindPollVote)
	add(h.pollClose, codec.KindPollClose)
	add(h.chatArchiveSet, codec.KindChatArchiveSet)
	add(h.draftSet, codec.KindDraftSet)
	add(h.reactionSet, codec.KindReactionSet)
	add(h.messagePinSet, codec.KindMessagePinSet)
	add(h.pinsGet, codec.KindPinsGet)
	add(h.postStatsGet, codec.KindPostStatsGet)
	add(h.historyGet, codec.KindHistoryGet)
	add(h.chatsGet, codec.KindChatsGet)
	add(h.schedulerRequest, codec.KindScheduledGet, codec.KindScheduledAdd, codec.KindScheduledEdit, codec.KindScheduledCancel, codec.KindScheduledSendNow)
	add(h.settingsRequest, codec.KindSettingsGet, codec.KindSettingsPut, codec.KindWallpapersGet)
	add(h.foldersRequest, codec.KindFoldersGet, codec.KindFoldersPut)
	add(h.agentRequest, codec.KindAgentsGet, codec.KindAgentSave, codec.KindAgentDelete, codec.KindAgentRunsGet)
	add(h.chatCreate, codec.KindChatCreate)
	add(h.chatJoin, codec.KindChatJoin)
	add(h.chatLeave, codec.KindChatLeave)
	add(h.chatMembersGet, codec.KindChatMembersGet)
	add(h.chatMemberAdd, codec.KindChatMemberAdd)
	add(h.chatMemberRemove, codec.KindChatMemberRemove)
	add(h.chatSearch, codec.KindChatSearch)
	add(h.chatResolve, codec.KindChatResolve)
	add(h.chatInviteReset, codec.KindChatInviteReset)
	add(h.userResolve, codec.KindUserResolve)
	add(h.chatPinSet, codec.KindChatPinSet)
	add(h.chatMuteSet, codec.KindChatMuteSet)

	return table
}

func (h *Handler) dispatch(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	next, found := h.table[request.Kind]
	if !found {
		return failure(request, codeInvalid, "unsupported kind")
	}

	return next(ctx, principal, request)
}
