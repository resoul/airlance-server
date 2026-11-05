package ws

import (
	"context"
	"errors"
	"log/slog"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/story"
	"go.airlance.org/server/internal/transport/codec"
)

type Stories interface {
	Publish(ctx context.Context, principal identity.Principal, draft story.Draft) (story.Story, error)
	Feed(ctx context.Context, principal identity.Principal) ([]story.Story, error)
	View(ctx context.Context, principal identity.Principal, id string) error
	Delete(ctx context.Context, principal identity.Principal, id string) error
}

func (h *Handler) storyPublish(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	next, err := codec.DecodeStoryPublish(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	stored, err := h.stories.Publish(ctx, principal, story.Draft{
		ClientID:      next.ClientID,
		MediaID:       next.MediaID,
		CoverMediaID:  next.CoverMediaID,
		IsVideo:       next.IsVideo,
		LifetimeHours: int(next.LifetimeHours),
		Meta:          next.Meta,
	})
	if err != nil {
		return h.storyFailure(ctx, request, err)
	}

	return reply(request, codec.KindStoryData, codec.EncodeStoryData(storyData(stored)))
}

func (h *Handler) storiesGet(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	stories, err := h.stories.Feed(ctx, principal)
	if err != nil {
		return h.storyFailure(ctx, request, err)
	}

	rows := make([]codec.StoryData, len(stories))
	for index, next := range stories {
		rows[index] = storyData(next)
	}

	return reply(request, codec.KindStoriesData, codec.EncodeStoriesData(rows))
}

func (h *Handler) storyView(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	id, err := codec.DecodeStoryView(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	if err := h.stories.View(ctx, principal, id); err != nil {
		return h.storyFailure(ctx, request, err)
	}

	return reply(request, codec.KindStoriesChanged, codec.EncodeStoriesChanged())
}

func (h *Handler) storyDelete(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	id, err := codec.DecodeStoryDelete(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	if err := h.stories.Delete(ctx, principal, id); err != nil {
		return h.storyFailure(ctx, request, err)
	}

	return reply(request, codec.KindStoriesChanged, codec.EncodeStoriesChanged())
}

func (h *Handler) storyFailure(ctx context.Context, request codec.Frame, err error) codec.Frame {
	switch {
	case errors.Is(err, identity.ErrForbidden):
		return failure(request, codeForbidden, "forbidden")
	case errors.Is(err, story.ErrNotFound):
		return failure(request, codeNotFound, "story_not_found")
	case errors.Is(err, story.ErrInvalidLifetime):
		return failure(request, codeUnprocessable, "invalid_lifetime")
	case errors.Is(err, story.ErrInvalidClientID):
		return failure(request, codeUnprocessable, "invalid_client_id")
	case errors.Is(err, story.ErrInvalidMeta):
		return failure(request, codeUnprocessable, "invalid_meta")
	case errors.Is(err, story.ErrInvalidMedia):
		return failure(request, codeUnprocessable, "invalid_media")
	default:
		h.logger.ErrorContext(ctx, "story request failed", slog.Any("error", err))

		return failure(request, codeInternal, "internal")
	}
}

func storyData(next story.Story) codec.StoryData {
	return codec.StoryData{
		StoryID:           next.ID,
		ClientID:          next.ClientID,
		OwnerSubject:      next.Owner.Subject,
		OwnerFirstName:    next.Owner.FirstName,
		OwnerLastName:     next.Owner.LastName,
		OwnerPhotoMediaID: next.Owner.PhotoMediaID,
		OwnerVideoMediaID: next.Owner.VideoMediaID,
		MediaID:           next.MediaID,
		CoverMediaID:      next.CoverMediaID,
		IsVideo:           next.IsVideo,
		Meta:              next.Meta,
		CreatedAtUnix:     next.CreatedAt.Unix(),
		ExpiresAtUnix:     next.ExpiresAt.Unix(),
		Seen:              next.Seen,
		ViewCount:         int32(next.ViewCount),
	}
}
