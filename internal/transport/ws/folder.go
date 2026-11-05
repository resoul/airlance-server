package ws

import (
	"context"
	"errors"
	"log/slog"

	"go.airlance.org/server/internal/domain/folder"
	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/transport/codec"
)

type Folders interface {
	Get(ctx context.Context, principal identity.Principal) (folder.List, error)
	Put(ctx context.Context, principal identity.Principal, list folder.List) (folder.List, error)
}

func (h *Handler) foldersRequest(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	if request.Kind == codec.KindFoldersGet {
		list, err := h.folders.Get(ctx, principal)

		return h.foldersReply(ctx, request, list, err)
	}

	put, err := codec.DecodeFoldersPut(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	list := folder.List{AllChatsPosition: put.AllChatsPosition, ShowsTags: put.ShowsTags}
	for _, next := range put.Folders {
		list.Folders = append(list.Folders, folder.Folder(next))
	}

	saved, err := h.folders.Put(ctx, principal, list)

	return h.foldersReply(ctx, request, saved, err)
}

func (h *Handler) foldersReply(ctx context.Context, request codec.Frame, list folder.List, err error) codec.Frame {
	switch {
	case err == nil:
	case errors.Is(err, identity.ErrForbidden):
		return failure(request, codeForbidden, "forbidden")
	case errors.Is(err, folder.ErrInvalid):
		return failure(request, codeUnprocessable, "invalid_folders")
	default:
		h.logger.ErrorContext(ctx, "folders request failed", slog.Any("error", err))

		return failure(request, codeInternal, "internal")
	}

	data := codec.FolderList{AllChatsPosition: list.AllChatsPosition, ShowsTags: list.ShowsTags, Revision: list.Revision}
	for _, next := range list.Folders {
		data.Folders = append(data.Folders, codec.Folder(next))
	}

	return reply(request, codec.KindFoldersData, codec.EncodeFoldersData(data))
}
