package ws

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/coder/websocket"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/update"
	"go.airlance.org/server/internal/transport/codec"
	"go.airlance.org/server/internal/transport/wireauth"
)

const updatesPage = 100

type Updates interface {
	Subscribe(ctx context.Context, principal identity.Principal, after uint64) (uint64, error)
	After(ctx context.Context, principal identity.Principal, after uint64, limit int) ([]update.Update, error)
	Listen(subject string) (<-chan struct{}, func())
}

type link struct {
	conn    *websocket.Conn
	channel *wireauth.Channel
	writing sync.Mutex
}

func (l *link) write(ctx context.Context, frame codec.Frame) error {
	l.writing.Lock()
	defer l.writing.Unlock()

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	defer cancel()

	return l.conn.Write(ctx, websocket.MessageBinary, l.channel.Seal(codec.EncodeFrame(frame)))
}

func (h *Handler) subscribe(
	ctx context.Context,
	l *link,
	principal identity.Principal,
	request codec.Frame,
	subscribed *bool,
) error {
	if *subscribed {
		return l.write(ctx, failure(request, codeConflict, "already_subscribed"))
	}

	after, err := codec.DecodeUpdatesSubscribe(request.Body)
	if err != nil {
		return l.write(ctx, failure(request, codeInvalid, "invalid body"))
	}

	latest, err := h.updates.Subscribe(ctx, principal, after)
	if err != nil {
		return l.write(ctx, h.subscribeFailure(ctx, request, err))
	}

	body := codec.EncodeUpdatesSubscribed(latest)
	if err := l.write(ctx, reply(request, codec.KindUpdatesSubscribed, body)); err != nil {
		return err
	}

	*subscribed = true

	go h.follow(ctx, l, principal, after)

	return nil
}

func (h *Handler) subscribeFailure(ctx context.Context, request codec.Frame, err error) codec.Frame {
	switch {
	case errors.Is(err, identity.ErrForbidden):
		return failure(request, codeForbidden, "forbidden")
	case errors.Is(err, update.ErrInvalidSeq):
		return failure(request, codeInvalid, "invalid_seq")
	default:
		h.logger.ErrorContext(ctx, "updates request failed", slog.Any("error", err))

		return failure(request, codeInternal, "internal")
	}
}

func (h *Handler) follow(ctx context.Context, l *link, principal identity.Principal, after uint64) {
	changes, stop := h.updates.Listen(principal.Subject)
	defer stop()

	for {
		next, err := h.push(ctx, l, principal, after)
		if err != nil {
			if ctx.Err() == nil {
				h.logger.ErrorContext(ctx, "updates delivery failed", slog.Any("error", err))
				_ = l.conn.Close(websocket.StatusTryAgainLater, "try later")
			}

			return
		}

		after = next

		select {
		case <-ctx.Done():
			return
		case <-changes:
		}
	}
}

func (h *Handler) push(ctx context.Context, l *link, principal identity.Principal, after uint64) (uint64, error) {
	for {
		page, err := h.updates.After(ctx, principal, after, updatesPage)
		if err != nil {
			return after, err
		}

		for _, next := range page {
			if err := l.write(ctx, updateFrame(next)); err != nil {
				return after, err
			}

			after = next.Seq
		}

		if len(page) < updatesPage {
			return after, nil
		}
	}
}

func updateFrame(next update.Update) codec.Frame {
	if next.Kind == update.KindFolders {
		return codec.Frame{Seq: next.Seq, Kind: codec.KindFoldersChanged, Body: codec.EncodeFoldersChanged()}
	}

	if next.Kind == update.KindScheduled {
		return codec.Frame{Seq: next.Seq, Kind: codec.KindScheduledChanged, Body: codec.EncodeScheduledChanged()}
	}

	if next.Kind == update.KindChats {
		return codec.Frame{Seq: next.Seq, Kind: codec.KindChatsChanged, Body: codec.EncodeChatsChanged()}
	}

	if next.Kind == update.KindContacts {
		return codec.Frame{Seq: next.Seq, Kind: codec.KindContactsChanged, Body: codec.EncodeContactsChanged()}
	}

	if next.Kind == update.KindStories {
		return codec.Frame{Seq: next.Seq, Kind: codec.KindStoriesChanged, Body: codec.EncodeStoriesChanged()}
	}

	if next.Kind == update.KindMessage {
		return codec.Frame{Seq: next.Seq, Kind: codec.KindMessageData, Body: messageBody(next.Message)}
	}

	return codec.Frame{Seq: next.Seq, Kind: codec.KindProfileData, Body: profileBody(next.Profile)}
}
