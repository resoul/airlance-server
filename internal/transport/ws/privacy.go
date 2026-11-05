package ws

import (
	"context"
	"errors"
	"log/slog"

	"go.airlance.org/server/internal/domain/directory"
	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/privacy"
	"go.airlance.org/server/internal/transport/codec"
)

type Privacy interface {
	Get(ctx context.Context, principal identity.Principal) (privacy.Settings, error)
	Set(ctx context.Context, principal identity.Principal, settings privacy.Settings) (privacy.Settings, error)
	Blocked(ctx context.Context, principal identity.Principal) ([]directory.Person, error)
	SetBlocked(ctx context.Context, principal identity.Principal, target string, blocked bool) ([]directory.Person, error)
}

func (h *Handler) privacyGet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	settings, err := h.privacy.Get(ctx, principal)
	if err != nil {
		return h.privacyFailure(ctx, request, err)
	}

	return reply(request, codec.KindPrivacyData, codec.EncodePrivacyData(privacyBody(settings)))
}

func (h *Handler) privacySet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	next, err := codec.DecodePrivacySet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	saved, err := h.privacy.Set(ctx, principal, privacy.Settings{
		Phone:    privacy.Level(next.Phone),
		Photos:   privacy.Level(next.Photos),
		Bio:      privacy.Level(next.Bio),
		Messages: privacy.Level(next.Messages),
		Search:   privacy.Level(next.Search),
	})
	if err != nil {
		return h.privacyFailure(ctx, request, err)
	}

	return reply(request, codec.KindPrivacyData, codec.EncodePrivacyData(privacyBody(saved)))
}

func (h *Handler) blocksGet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	people, err := h.privacy.Blocked(ctx, principal)
	if err != nil {
		return h.privacyFailure(ctx, request, err)
	}

	return reply(request, codec.KindBlocksData, codec.EncodeBlocksData(blockedBody(people)))
}

func (h *Handler) blockSet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	subject, blocked, err := codec.DecodeBlockSet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	people, err := h.privacy.SetBlocked(ctx, principal, subject, blocked)
	if err != nil {
		return h.privacyFailure(ctx, request, err)
	}

	return reply(request, codec.KindBlocksData, codec.EncodeBlocksData(blockedBody(people)))
}

func (h *Handler) privacyFailure(ctx context.Context, request codec.Frame, err error) codec.Frame {
	switch {
	case errors.Is(err, identity.ErrForbidden):
		return failure(request, codeForbidden, "forbidden")
	case errors.Is(err, privacy.ErrInvalidSettings):
		return failure(request, codeUnprocessable, "invalid_settings")
	case errors.Is(err, privacy.ErrInvalidPeer):
		return failure(request, codeUnprocessable, "invalid_peer")
	default:
		h.logger.ErrorContext(ctx, "privacy request failed", slog.Any("error", err))

		return failure(request, codeInternal, "internal")
	}
}

func privacyBody(settings privacy.Settings) codec.PrivacySettings {
	return codec.PrivacySettings{
		Phone:    int32(settings.Phone),
		Photos:   int32(settings.Photos),
		Bio:      int32(settings.Bio),
		Messages: int32(settings.Messages),
		Search:   int32(settings.Search),
	}
}

func blockedBody(people []directory.Person) []codec.SearchPerson {
	rows := make([]codec.SearchPerson, len(people))
	for index, person := range people {
		rows[index] = codec.SearchPerson{
			Subject:   person.Subject,
			FirstName: person.FirstName,
			LastName:  person.LastName,
			Username:  person.Username,
		}
	}

	return rows
}
