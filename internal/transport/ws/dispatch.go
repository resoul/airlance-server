package ws

import (
	"context"
	"errors"
	"log/slog"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/profile"
	"go.airlance.org/server/internal/transport/codec"
)

const (
	codeInvalid         int32 = 400
	codeUnauthenticated int32 = 401
	codeForbidden       int32 = 403
	codeNotFound        int32 = 404
	codeConflict        int32 = 409
	codeUnprocessable   int32 = 422
	codeTooManyRequests int32 = 429
	codeInternal        int32 = 500
	codeUnavailable     int32 = 503
)

type Profiles interface {
	GetOwn(ctx context.Context, principal identity.Principal) (profile.Profile, error)
	GetPublic(ctx context.Context, principal identity.Principal, subject string) (profile.Profile, error)
	UpdateOwn(ctx context.Context, principal identity.Principal, fields profile.Fields) (profile.Profile, error)
	AddPhoto(ctx context.Context, principal identity.Principal, photoMediaID, videoMediaID string) (profile.Profile, error)
	RemovePhoto(ctx context.Context, principal identity.Principal, photoMediaID string) (profile.Profile, error)
}

func (h *Handler) dispatch(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	switch request.Kind {
	case codec.KindPing:
		id, err := codec.DecodePing(request.Body)
		if err != nil {
			return failure(request, codeInvalid, "invalid body")
		}

		return reply(request, codec.KindPong, codec.EncodePong(id))
	case codec.KindProfileGet:
		got, err := h.profiles.GetOwn(ctx, principal)

		return h.profileReply(ctx, request, got, err)
	case codec.KindProfileSet:
		fields, err := codec.DecodeProfileSet(request.Body)
		if err != nil {
			return failure(request, codeInvalid, "invalid body")
		}

		got, err := h.profiles.UpdateOwn(ctx, principal, profile.Fields(fields))

		return h.profileReply(ctx, request, got, err)
	case codec.KindProfilePhotoAdd:
		photo, video, err := codec.DecodeProfilePhotoAdd(request.Body)
		if err != nil {
			return failure(request, codeInvalid, "invalid body")
		}

		got, err := h.profiles.AddPhoto(ctx, principal, photo, video)

		return h.profileReply(ctx, request, got, err)
	case codec.KindProfilePhotoRemove:
		photo, err := codec.DecodeProfilePhotoRemove(request.Body)
		if err != nil {
			return failure(request, codeInvalid, "invalid body")
		}

		got, err := h.profiles.RemovePhoto(ctx, principal, photo)

		return h.profileReply(ctx, request, got, err)
	case codec.KindMediaUploadRequest:
		return h.mediaUpload(ctx, principal, request)
	case codec.KindMediaDownloadRequest:
		return h.mediaDownload(ctx, principal, request)
	case codec.KindSearch:
		return h.search(ctx, principal, request)
	case codec.KindContactsGet, codec.KindContactAdd, codec.KindContactAddPhone,
		codec.KindContactRemove, codec.KindContactsSync:
		return h.contactsRequest(ctx, principal, request)
	case codec.KindPrivacyGet:
		return h.privacyGet(ctx, principal, request)
	case codec.KindPrivacySet:
		return h.privacySet(ctx, principal, request)
	case codec.KindBlocksGet:
		return h.blocksGet(ctx, principal, request)
	case codec.KindBlockSet:
		return h.blockSet(ctx, principal, request)
	case codec.KindStoryPublish:
		return h.storyPublish(ctx, principal, request)
	case codec.KindStoriesGet:
		return h.storiesGet(ctx, principal, request)
	case codec.KindStoryView:
		return h.storyView(ctx, principal, request)
	case codec.KindStoryDelete:
		return h.storyDelete(ctx, principal, request)
	case codec.KindChatOpenDirect:
		return h.chatOpenDirect(ctx, principal, request)
	case codec.KindMessageSend:
		return h.messageSend(ctx, principal, request)
	case codec.KindHistoryGet:
		return h.historyGet(ctx, principal, request)
	case codec.KindChatsGet:
		return h.chatsGet(ctx, principal, request)
	case codec.KindPublicProfileGet:
		subject, err := codec.DecodePublicProfileGet(request.Body)
		if err != nil {
			return failure(request, codeInvalid, "invalid body")
		}

		got, err := h.profiles.GetPublic(ctx, principal, subject)

		return h.profileReply(ctx, request, got, err)
	default:
		return failure(request, codeInvalid, "unsupported kind")
	}
}

func (h *Handler) profileReply(ctx context.Context, request codec.Frame, got profile.Profile, err error) codec.Frame {
	switch {
	case err == nil:
		return reply(request, codec.KindProfileData, profileBody(got))
	case errors.Is(err, identity.ErrForbidden):
		return failure(request, codeForbidden, "forbidden")
	case errors.Is(err, profile.ErrNotFound):
		return failure(request, codeNotFound, "profile_not_found")
	case errors.Is(err, profile.ErrUsernameTaken):
		return failure(request, codeConflict, "username_taken")
	case errors.Is(err, profile.ErrInvalidName):
		return failure(request, codeUnprocessable, "invalid_name")
	case errors.Is(err, profile.ErrInvalidUsername):
		return failure(request, codeUnprocessable, "invalid_username")
	case errors.Is(err, profile.ErrInvalidBio):
		return failure(request, codeUnprocessable, "invalid_bio")
	case errors.Is(err, profile.ErrInvalidPhone):
		return failure(request, codeUnprocessable, "invalid_phone")
	case errors.Is(err, profile.ErrInvalidMedia):
		return failure(request, codeUnprocessable, "invalid_media")
	case errors.Is(err, profile.ErrTooManyPhotos):
		return failure(request, codeUnprocessable, "too_many_photos")
	default:
		h.logger.ErrorContext(ctx, "profile request failed", slog.Any("error", err))

		return failure(request, codeInternal, "internal")
	}
}

func profileBody(got profile.Profile) []byte {
	photos := make([]codec.ProfilePhoto, len(got.Photos))
	for index, photo := range got.Photos {
		photos[index] = codec.ProfilePhoto{
			PhotoMediaID: photo.PhotoMediaID,
			VideoMediaID: photo.VideoMediaID,
			AddedAtUnix:  photo.AddedAt.Unix(),
		}
	}

	return codec.EncodeProfileData(codec.ProfileData{
		Subject:       got.Subject,
		Fields:        codec.ProfileFields(got.Fields),
		Photos:        photos,
		UpdatedAtUnix: got.UpdatedAt.Unix(),
	})
}

func reply(request codec.Frame, kind codec.Kind, body []byte) codec.Frame {
	return codec.Frame{RequestID: request.RequestID, Kind: kind, Body: body}
}

func failure(request codec.Frame, code int32, message string) codec.Frame {
	return reply(request, codec.KindError, codec.EncodeError(code, message))
}
