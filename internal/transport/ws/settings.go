package ws

import (
	"context"
	"errors"
	"log/slog"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/settings"
	"go.airlance.org/server/internal/transport/codec"
)

type Settings interface {
	Get(ctx context.Context, principal identity.Principal, key string) (settings.Record, error)
	Put(ctx context.Context, principal identity.Principal, key string, data []byte) (settings.Record, error)
	Wallpapers(ctx context.Context, principal identity.Principal) ([]settings.Wallpaper, error)
}

func (h *Handler) settingsRequest(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	switch request.Kind {
	case codec.KindSettingsGet:
		key, err := codec.DecodeSettingsGet(request.Body)
		if err != nil {
			return failure(request, codeInvalid, "invalid body")
		}

		record, err := h.settings.Get(ctx, principal, key)

		return h.settingsReply(ctx, request, record, err)
	case codec.KindSettingsPut:
		key, data, err := codec.DecodeSettingsPut(request.Body)
		if err != nil {
			return failure(request, codeInvalid, "invalid body")
		}

		record, err := h.settings.Put(ctx, principal, key, data)

		return h.settingsReply(ctx, request, record, err)
	default:
		return h.wallpapersReply(ctx, principal, request)
	}
}

func (h *Handler) settingsReply(ctx context.Context, request codec.Frame, record settings.Record, err error) codec.Frame {
	if err != nil {
		return h.settingsFailure(ctx, request, err)
	}

	return reply(request, codec.KindSettingsData, settingsBody(record))
}

func (h *Handler) wallpapersReply(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	list, err := h.settings.Wallpapers(ctx, principal)
	if err != nil {
		return h.settingsFailure(ctx, request, err)
	}

	items := make([]codec.Wallpaper, len(list))
	for index, next := range list {
		items[index] = codec.Wallpaper{ID: next.ID, Name: next.Name, MediaID: next.MediaID}
	}

	return reply(request, codec.KindWallpapers, codec.EncodeWallpapers(items))
}

func (h *Handler) settingsFailure(ctx context.Context, request codec.Frame, err error) codec.Frame {
	switch {
	case errors.Is(err, identity.ErrForbidden):
		return failure(request, codeForbidden, "forbidden")
	case errors.Is(err, settings.ErrInvalid):
		return failure(request, codeUnprocessable, "invalid_settings")
	default:
		h.logger.ErrorContext(ctx, "settings request failed", slog.Any("error", err))

		return failure(request, codeInternal, "internal")
	}
}

func settingsBody(record settings.Record) []byte {
	return codec.EncodeSettingsData(codec.SettingsData{Key: record.Key, Version: record.Version, Data: record.Data})
}
