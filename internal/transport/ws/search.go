package ws

import (
	"context"
	"errors"
	"log/slog"

	"go.airlance.org/server/internal/domain/directory"
	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/transport/codec"
)

type Directory interface {
	Search(ctx context.Context, principal identity.Principal, query string) ([]directory.Person, error)
}

func (h *Handler) search(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	query, err := codec.DecodeSearch(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	people, err := h.directory.Search(ctx, principal, query)
	if err != nil {
		return h.searchFailure(ctx, request, err)
	}

	found := make([]codec.SearchPerson, len(people))
	for index, person := range people {
		found[index] = codec.SearchPerson{
			Subject:      person.Subject,
			FirstName:    person.FirstName,
			LastName:     person.LastName,
			Username:     person.Username,
			PhotoMediaID: person.PhotoMediaID,
			VideoMediaID: person.VideoMediaID,
		}
	}

	return reply(request, codec.KindSearchResults, codec.EncodeSearchResults(found))
}

func (h *Handler) searchFailure(ctx context.Context, request codec.Frame, err error) codec.Frame {
	switch {
	case errors.Is(err, identity.ErrForbidden):
		return failure(request, codeForbidden, "forbidden")
	case errors.Is(err, directory.ErrInvalidQuery):
		return failure(request, codeUnprocessable, "invalid_query")
	case errors.Is(err, directory.ErrTooManySearches):
		return failure(request, codeTooManyRequests, "too_many_searches")
	case errors.Is(err, directory.ErrSearchUnavailable):
		h.logger.ErrorContext(ctx, "search failed", slog.Any("error", err))

		return failure(request, codeUnavailable, "search_unavailable")
	default:
		h.logger.ErrorContext(ctx, "search request failed", slog.Any("error", err))

		return failure(request, codeInternal, "internal")
	}
}
