package ws

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/coder/websocket"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/transport/codec"
	"go.airlance.org/server/internal/transport/wireauth"
	"go.airlance.org/server/internal/usecase/session"
)

const (
	StatusTokenExpired websocket.StatusCode = 4401

	writeTimeout       = 10 * time.Second
	unauthenticatedTTL = 15 * time.Second
	unauthenticatedMax = 32 << 10
	realIPHeader       = "CF-Connecting-IP"
	frameOverhead      = 64
)

type Authenticator interface {
	Authenticate(ctx context.Context, token string) (identity.Principal, error)
}

type Handshaker interface {
	Perform(conn wireauth.MessageReadWriter) (*wireauth.Channel, error)
}

type Services struct {
	Profiles  Profiles
	Updates   Updates
	Media     Media
	Stories   Stories
	Privacy   Privacy
	Contacts  Contacts
	Directory Directory
	Messaging Messaging
}

type Handler struct {
	root      context.Context
	secure    Handshaker
	auth      Authenticator
	profiles  Profiles
	updates   Updates
	media     Media
	stories   Stories
	privacy   Privacy
	contacts  Contacts
	directory Directory
	messaging Messaging
	limits    *limiter
	logger    *slog.Logger
}

func NewHandler(
	root context.Context,
	secure Handshaker,
	auth Authenticator,
	services Services,
	limits Limits,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		root:      root,
		secure:    secure,
		auth:      auth,
		profiles:  services.Profiles,
		updates:   services.Updates,
		media:     services.Media,
		stories:   services.Stories,
		privacy:   services.Privacy,
		contacts:  services.Contacts,
		directory: services.Directory,
		messaging: services.Messaging,
		limits:    newLimiter(limits),
		logger:    logger,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	address := clientAddress(r)

	release, allowed := h.limits.admit(address, time.Now())
	if !allowed {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "too many connections", http.StatusTooManyRequests)

		return
	}
	defer release()

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		h.logger.InfoContext(r.Context(), "websocket refused", slog.Any("error", err))

		return
	}

	conn.SetReadLimit(unauthenticatedMax)

	opened, principal, status, reason := h.open(conn)
	if status != 0 {
		_ = conn.Close(status, reason)

		return
	}

	releaseSubject, allowed := h.limits.admitSubject(principal.Subject)
	if !allowed {
		_ = conn.Close(websocket.StatusPolicyViolation, "too many connections")

		return
	}
	defer releaseSubject()

	conn.SetReadLimit(codec.MaxFrameBytes + frameOverhead)
	h.serve(opened, principal)
}

func (h *Handler) open(conn *websocket.Conn) (*link, identity.Principal, websocket.StatusCode, string) {
	ctx, cancel := context.WithTimeout(h.root, unauthenticatedTTL)
	defer cancel()

	channel, err := h.secure.Perform(socket{ctx: ctx, conn: conn})
	if err != nil {
		h.logger.DebugContext(ctx, "handshake failed", slog.Any("error", err))

		return nil, identity.Principal{}, websocket.StatusPolicyViolation, "handshake failed"
	}

	_, sealed, err := conn.Read(ctx)
	if err != nil {
		return nil, identity.Principal{}, websocket.StatusPolicyViolation, "authentication timed out"
	}

	request, token, err := readAuthenticate(channel, sealed)
	if err != nil {
		return nil, identity.Principal{}, websocket.StatusPolicyViolation, "authenticate first"
	}

	principal, err := h.auth.Authenticate(ctx, token)
	status, reason, response := authReply(request, principal, err)
	if status != 0 {
		h.logRefusal(ctx, err)
	}

	opened := &link{conn: conn, channel: channel}
	if err := opened.write(ctx, response); err != nil {
		return nil, identity.Principal{}, websocket.StatusInternalError, ""
	}

	return opened, principal, status, reason
}

func (h *Handler) logRefusal(ctx context.Context, err error) {
	if !errors.Is(err, session.ErrInvalidToken) {
		h.logger.ErrorContext(ctx, "token check failed", slog.Any("error", err))
	}
}

func readAuthenticate(channel *wireauth.Channel, sealed []byte) (codec.Frame, string, error) {
	plain, err := channel.Open(sealed)
	if err != nil {
		return codec.Frame{}, "", err
	}

	request, err := codec.DecodeFrame(plain)
	if err != nil {
		return codec.Frame{}, "", err
	}

	if request.Kind != codec.KindAuthenticate {
		return codec.Frame{}, "", codec.ErrInvalid
	}

	token, err := codec.DecodeAuthenticate(request.Body)

	return request, token, err
}

func authReply(
	request codec.Frame,
	principal identity.Principal,
	err error,
) (websocket.StatusCode, string, codec.Frame) {
	switch {
	case err == nil:
		body := codec.EncodeAuthenticated(principal.ExpiresAt.Unix())

		return 0, "", reply(request, codec.KindAuthenticated, body)
	case errors.Is(err, session.ErrInvalidToken):
		return StatusTokenExpired, "invalid token", failure(request, codeUnauthenticated, "unauthenticated")
	default:
		return websocket.StatusTryAgainLater, "try later", failure(request, codeUnavailable, "unavailable")
	}
}

func (h *Handler) serve(l *link, principal identity.Principal) {
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

			_ = l.conn.Close(status, reason)
		}
	}()

	if status, reason := h.loop(ctx, l, principal); ctx.Err() == nil {
		if err := l.conn.Close(status, reason); err != nil {
			h.logger.DebugContext(ctx, "websocket close", slog.Any("error", err))
		}
	}
}

func (h *Handler) loop(ctx context.Context, l *link, principal identity.Principal) (websocket.StatusCode, string) {
	reads := context.WithoutCancel(ctx)
	rate := h.limits.newFrameRate(time.Now())
	following, stop := context.WithCancel(ctx)
	defer stop()

	subscribed := false
	for {
		kind, sealed, err := l.conn.Read(reads)
		switch {
		case err != nil:
			return websocket.StatusNormalClosure, ""
		case kind != websocket.MessageBinary:
			return websocket.StatusUnsupportedData, "binary frames only"
		case !rate.allow(time.Now()):
			return websocket.StatusPolicyViolation, "too many frames"
		}

		plain, err := l.channel.Open(sealed)
		if err != nil {
			return websocket.StatusPolicyViolation, "invalid packet"
		}

		request, err := codec.DecodeFrame(plain)
		if err != nil {
			return websocket.StatusInvalidFramePayloadData, "invalid frame"
		}

		if request.Kind == codec.KindUpdatesSubscribe {
			err = h.subscribe(following, l, principal, request, &subscribed)
		} else {
			err = l.write(ctx, h.dispatch(ctx, principal, request))
		}

		if err != nil {
			return websocket.StatusInternalError, ""
		}
	}
}

type socket struct {
	ctx  context.Context
	conn *websocket.Conn
}

func (s socket) ReadMessage() (int, []byte, error) {
	kind, payload, err := s.conn.Read(s.ctx)
	if err != nil {
		return 0, nil, err
	}

	if kind != websocket.MessageBinary {
		return int(kind), payload, nil
	}

	return wireauth.BinaryMessageType, payload, nil
}

func (s socket) WriteMessage(_ int, payload []byte) error {
	return s.conn.Write(s.ctx, websocket.MessageBinary, payload)
}

func clientAddress(r *http.Request) string {
	if address, err := netip.ParseAddr(r.Header.Get(realIPHeader)); err == nil {
		return address.String()
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}
