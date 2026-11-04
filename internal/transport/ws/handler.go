package ws

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/transport/codec"
	"go.airlance.org/server/internal/usecase/session"
)

const (
	StatusTokenExpired websocket.StatusCode = 4401

	writeTimeout = 10 * time.Second
)

type Authenticator interface {
	Authenticate(ctx context.Context, token string) (identity.Principal, error)
}

type Handler struct {
	root     context.Context
	auth     Authenticator
	profiles Profiles
	logger   *slog.Logger
}

func NewHandler(root context.Context, auth Authenticator, profiles Profiles, logger *slog.Logger) *Handler {
	return &Handler{root: root, auth: auth, profiles: profiles, logger: logger}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !found || token == "" {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthenticated", http.StatusUnauthorized)

		return
	}

	principal, err := h.auth.Authenticate(r.Context(), token)
	switch {
	case errors.Is(err, session.ErrInvalidToken):
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		http.Error(w, "unauthenticated", http.StatusUnauthorized)

		return
	case err != nil:
		h.logger.ErrorContext(r.Context(), "token check failed", slog.Any("error", err))
		http.Error(w, "unavailable", http.StatusServiceUnavailable)

		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		h.logger.InfoContext(r.Context(), "websocket refused", slog.Any("error", err))

		return
	}

	conn.SetReadLimit(codec.MaxFrameBytes)
	h.serve(conn, principal)
}

func (h *Handler) serve(conn *websocket.Conn, principal identity.Principal) {
	ctx, cancel := context.WithDeadline(h.root, principal.ExpiresAt)
	defer cancel()

	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-done:
		case <-ctx.Done():
			status, reason := websocket.StatusGoingAway, "server stopping"
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				status, reason = StatusTokenExpired, "token expired"
			}

			_ = conn.Close(status, reason)
		}
	}()

	if status, reason := h.loop(ctx, conn, principal); ctx.Err() == nil {
		if err := conn.Close(status, reason); err != nil {
			h.logger.DebugContext(ctx, "websocket close", slog.Any("error", err))
		}
	}
}

func (h *Handler) loop(ctx context.Context, conn *websocket.Conn, principal identity.Principal) (websocket.StatusCode, string) {
	reads := context.WithoutCancel(ctx)
	for {
		kind, data, err := conn.Read(reads)
		switch {
		case err != nil:
			return websocket.StatusNormalClosure, ""
		case kind != websocket.MessageBinary:
			return websocket.StatusUnsupportedData, "binary frames only"
		}

		request, err := codec.DecodeFrame(data)
		if err != nil {
			return websocket.StatusInvalidFramePayloadData, "invalid frame"
		}

		response := h.dispatch(ctx, principal, request)
		if err := h.write(ctx, conn, response); err != nil {
			return websocket.StatusInternalError, ""
		}
	}
}

func (h *Handler) write(ctx context.Context, conn *websocket.Conn, frame codec.Frame) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()

	return conn.Write(ctx, websocket.MessageBinary, codec.EncodeFrame(frame))
}
