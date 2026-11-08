package ws

import (
	"context"
	"errors"
	"log/slog"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/profile"
	"go.airlance.org/server/internal/transport/codec"
)

func (h *Handler) ping(_ context.Context, _ identity.Principal, request codec.Frame) codec.Frame {
	id, err := codec.DecodePing(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	return reply(request, codec.KindPong, codec.EncodePong(id))
}

func (h *Handler) profileGet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	got, err := h.profiles.GetOwn(ctx, principal)

	return h.profileReply(ctx, request, got, err)
}

func (h *Handler) profileSet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	fields, err := codec.DecodeProfileSet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	got, err := h.profiles.UpdateOwn(ctx, principal, profile.Fields(fields))

	return h.profileReply(ctx, request, got, err)
}

func (h *Handler) profilePhotoAdd(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	photo, video, err := codec.DecodeProfilePhotoAdd(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	got, err := h.profiles.AddPhoto(ctx, principal, photo, video)

	return h.profileReply(ctx, request, got, err)
}

func (h *Handler) profilePhotoRemove(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	photo, err := codec.DecodeProfilePhotoRemove(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	got, err := h.profiles.RemovePhoto(ctx, principal, photo)

	return h.profileReply(ctx, request, got, err)
}

func (h *Handler) publicProfileGet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	subject, err := codec.DecodePublicProfileGet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	got, err := h.profiles.GetPublic(ctx, principal, subject)

	return h.profileReply(ctx, request, got, err)
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
