package ws

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/transport/codec"
)

func TestEveryClientRequestHasARoute(t *testing.T) {
	t.Parallel()

	handler := NewHandler(context.Background(), nil, nil, Services{}, DefaultLimits(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	wanted := []codec.Kind{
		codec.KindPing,
		codec.KindProfileGet,
		codec.KindProfileSet,
		codec.KindProfilePhotoAdd,
		codec.KindProfilePhotoRemove,
		codec.KindPublicProfileGet,
		codec.KindMediaUploadRequest,
		codec.KindMediaDownloadRequest,
		codec.KindSearch,
		codec.KindContactsGet,
		codec.KindContactAdd,
		codec.KindContactAddPhone,
		codec.KindContactRemove,
		codec.KindContactsSync,
		codec.KindPrivacyGet,
		codec.KindPrivacySet,
		codec.KindBlocksGet,
		codec.KindBlockSet,
		codec.KindStoryPublish,
		codec.KindStoriesGet,
		codec.KindStoryView,
		codec.KindStoryDelete,
		codec.KindChatOpenDirect,
		codec.KindMessageSend,
		codec.KindMessageEdit,
		codec.KindMessageDelete,
		codec.KindChatReadSet,
		codec.KindChatMemberUnban,
		codec.KindChatBansGet,
		codec.KindChatInfoSet,
		codec.KindChatRulesSet,
		codec.KindChatLogGet,
		codec.KindLinkPreviewGet,
		codec.KindChatReadersGet,
		codec.KindTypingSet,
		codec.KindChatRightsSet,
		codec.KindChatDeliveredSet,
		codec.KindPollVote,
		codec.KindPollClose,
		codec.KindChatArchiveSet,
		codec.KindDraftSet,
		codec.KindReactionSet,
		codec.KindMessagePinSet,
		codec.KindPinsGet,
		codec.KindPostStatsGet,
		codec.KindHistoryGet,
		codec.KindChatsGet,
		codec.KindScheduledGet,
		codec.KindScheduledAdd,
		codec.KindScheduledEdit,
		codec.KindScheduledCancel,
		codec.KindScheduledSendNow,
		codec.KindSettingsGet,
		codec.KindSettingsPut,
		codec.KindWallpapersGet,
		codec.KindFoldersGet,
		codec.KindFoldersPut,
		codec.KindAgentsGet,
		codec.KindAgentSave,
		codec.KindAgentDelete,
		codec.KindAgentRunsGet,
		codec.KindChatCreate,
		codec.KindChatJoin,
		codec.KindChatLeave,
		codec.KindChatMembersGet,
		codec.KindChatMemberAdd,
		codec.KindChatMemberRemove,
		codec.KindChatSearch,
		codec.KindChatResolve,
		codec.KindChatInviteReset,
		codec.KindUserResolve,
		codec.KindChatPinSet,
		codec.KindChatMuteSet,
	}

	for _, kind := range wanted {
		if handler.table[kind] == nil {
			t.Errorf("kind %d has no route", kind)
		}
	}

	if len(handler.table) != len(wanted) {
		t.Errorf("the table has %d routes, the test lists %d: update the list", len(handler.table), len(wanted))
	}
}

func TestAnUnknownKindIsRefused(t *testing.T) {
	t.Parallel()

	handler := NewHandler(context.Background(), nil, nil, Services{}, DefaultLimits(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	got := handler.dispatch(context.Background(), identity.Principal{}, codec.Frame{RequestID: 7, Kind: codec.Kind(60000)})

	if got.Kind != codec.KindError || got.RequestID != 7 {
		t.Fatalf("got %+v", got)
	}
}
