package ws

import (
	"context"
	"errors"
	"log/slog"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/media"
	"go.airlance.org/server/internal/transport/codec"
	mediausecase "go.airlance.org/server/internal/usecase/media"
)

type Media interface {
	RequestUpload(ctx context.Context, principal identity.Principal, size int64) (mediausecase.Ticket, error)
	RequestDownload(ctx context.Context, principal identity.Principal, id string) (mediausecase.Ticket, error)
}

func (h *Handler) mediaUpload(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	size, err := codec.DecodeMediaUploadRequest(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	ticket, err := h.media.RequestUpload(ctx, principal, size)
	if err != nil {
		return h.mediaFailure(ctx, request, err)
	}

	return reply(request, codec.KindMediaUploadTicket, codec.EncodeMediaUploadTicket(codec.MediaUploadTicket{
		MediaID:       ticket.MediaID,
		URL:           ticket.URL,
		ExpiresAtUnix: ticket.ExpiresAt.Unix(),
	}))
}

func (h *Handler) mediaDownload(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	id, err := codec.DecodeMediaDownloadRequest(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	ticket, err := h.media.RequestDownload(ctx, principal, id)
	if err != nil {
		return h.mediaFailure(ctx, request, err)
	}

	return reply(request, codec.KindMediaDownloadTicket, codec.EncodeMediaDownloadTicket(codec.MediaDownloadTicket{
		URL:           ticket.URL,
		ExpiresAtUnix: ticket.ExpiresAt.Unix(),
	}))
}

func (h *Handler) mediaFailure(ctx context.Context, request codec.Frame, err error) codec.Frame {
	switch {
	case errors.Is(err, media.ErrInvalidSize):
		return failure(request, codeUnprocessable, "invalid_size")
	case errors.Is(err, media.ErrNotFound):
		return failure(request, codeNotFound, "media_not_found")
	default:
		h.logger.ErrorContext(ctx, "media request failed", slog.Any("error", err))

		return failure(request, codeInternal, "internal")
	}
}
