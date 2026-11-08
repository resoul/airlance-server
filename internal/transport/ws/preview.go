package ws

import (
	"context"
	"errors"
	"log/slog"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/linkpreview"
	"go.airlance.org/server/internal/transport/codec"
)

type LinkPreviews interface {
	Preview(ctx context.Context, principal identity.Principal, link string) (linkpreview.Preview, error)
}

func (h *Handler) linkPreviewGet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	link, err := codec.DecodeLinkPreviewGet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	preview, err := h.previews.Preview(ctx, principal, link)
	if err != nil {
		return h.previewFailure(ctx, request, err)
	}

	return reply(request, codec.KindLinkPreviewData, codec.EncodeLinkPreviewData(codec.LinkPreviewData{
		Found: preview.Found, URL: preview.URL, Site: preview.Site, Title: preview.Title, Summary: preview.Summary,
		PhotoMediaID: preview.PhotoMediaID, Width: preview.Width, Height: preview.Height,
	}))
}

func (h *Handler) previewFailure(ctx context.Context, request codec.Frame, err error) codec.Frame {
	switch {
	case errors.Is(err, identity.ErrForbidden):
		return failure(request, codeForbidden, "forbidden")
	case errors.Is(err, linkpreview.ErrInvalidURL):
		return failure(request, codeUnprocessable, "invalid_link")
	case errors.Is(err, linkpreview.ErrRateLimited):
		return failure(request, codeTooManyRequests, "too_many_previews")
	default:
		h.logger.ErrorContext(ctx, "link preview failed", slog.Any("error", err))

		return failure(request, codeInternal, "internal")
	}
}
