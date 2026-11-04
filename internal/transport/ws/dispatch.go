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
	codeInvalid       int32 = 400
	codeForbidden     int32 = 403
	codeNotFound      int32 = 404
	codeConflict      int32 = 409
	codeUnprocessable int32 = 422
	codeInternal      int32 = 500
)

type Profiles interface {
	GetOwn(ctx context.Context, principal identity.Principal) (profile.Profile, error)
	UpdateOwn(ctx context.Context, principal identity.Principal, fields profile.Fields) (profile.Profile, error)
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
	default:
		return failure(request, codeInvalid, "unsupported kind")
	}
}

func (h *Handler) profileReply(ctx context.Context, request codec.Frame, got profile.Profile, err error) codec.Frame {
	switch {
	case err == nil:
		return reply(request, codec.KindProfileData, codec.EncodeProfileData(codec.ProfileData{
			Subject:       got.Subject,
			Fields:        codec.ProfileFields(got.Fields),
			UpdatedAtUnix: got.UpdatedAt.Unix(),
		}))
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
	default:
		h.logger.ErrorContext(ctx, "profile request failed", slog.Any("error", err))

		return failure(request, codeInternal, "internal")
	}
}

func reply(request codec.Frame, kind codec.Kind, body []byte) codec.Frame {
	return codec.Frame{RequestID: request.RequestID, Kind: kind, Body: body}
}

func failure(request codec.Frame, code int32, message string) codec.Frame {
	return reply(request, codec.KindError, codec.EncodeError(code, message))
}
