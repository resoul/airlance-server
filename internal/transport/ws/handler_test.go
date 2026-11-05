package ws_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"go.airlance.org/server/internal/domain/contact"
	"go.airlance.org/server/internal/domain/directory"
	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/privacy"
	"go.airlance.org/server/internal/domain/profile"
	"go.airlance.org/server/internal/domain/story"
	"go.airlance.org/server/internal/domain/update"
	"go.airlance.org/server/internal/transport/codec"
	"go.airlance.org/server/internal/transport/wireauth"
	"go.airlance.org/server/internal/transport/ws"
	"go.airlance.org/server/internal/usecase/session"
	updatesusecase "go.airlance.org/server/internal/usecase/update"
)

const goodToken = "good"

type fakeAuth struct {
	expiresIn time.Duration
}

func (a fakeAuth) Authenticate(_ context.Context, token string) (identity.Principal, error) {
	switch token {
	case goodToken:
		return identity.Principal{Subject: "anna", ExpiresAt: time.Now().Add(a.expiresIn)}, nil
	case "down":
		return identity.Principal{}, session.ErrUnavailable
	default:
		return identity.Principal{}, session.ErrInvalidToken
	}
}

type fakeProfiles struct{}

func (fakeProfiles) GetOwn(context.Context, identity.Principal) (profile.Profile, error) {
	return profile.Profile{}, profile.ErrNotFound
}

func (fakeProfiles) GetPublic(_ context.Context, _ identity.Principal, subject string) (profile.Profile, error) {
	if subject == "missing" {
		return profile.Profile{}, profile.ErrNotFound
	}

	return profile.Profile{Subject: subject, Fields: profile.Fields{FirstName: "Петро"}, UpdatedAt: time.Unix(5, 0)}, nil
}

func (fakeProfiles) AddPhoto(_ context.Context, principal identity.Principal, photo, video string) (profile.Profile, error) {
	return profile.Profile{Subject: principal.Subject, Photos: []profile.Photo{
		{PhotoMediaID: photo, VideoMediaID: video, AddedAt: time.Unix(7, 0)},
	}, UpdatedAt: time.Unix(5, 0)}, nil
}

func (fakeProfiles) RemovePhoto(_ context.Context, principal identity.Principal, _ string) (profile.Profile, error) {
	return profile.Profile{Subject: principal.Subject, UpdatedAt: time.Unix(5, 0)}, nil
}

type fakeContacts struct{}

func (fakeContacts) List(context.Context, identity.Principal) ([]contact.Contact, error) {
	return []contact.Contact{{Subject: "a"}}, nil
}

func (fakeContacts) Add(_ context.Context, _ identity.Principal, subject, _, _ string) ([]contact.Contact, error) {
	return []contact.Contact{{Subject: subject}}, nil
}

func (fakeContacts) AddByPhone(context.Context, identity.Principal, string, string, string) ([]contact.Contact, error) {
	return nil, contact.ErrNotFound
}

func (fakeContacts) Remove(context.Context, identity.Principal, string) ([]contact.Contact, error) {
	return nil, nil
}

func (fakeContacts) Sync(context.Context, identity.Principal, []string) ([]contact.Contact, error) {
	return nil, nil
}

type fakePrivacy struct{}

func (fakePrivacy) Get(context.Context, identity.Principal) (privacy.Settings, error) {
	return privacy.Defaults(), nil
}

func (fakePrivacy) Set(_ context.Context, _ identity.Principal, settings privacy.Settings) (privacy.Settings, error) {
	return settings, nil
}

func (fakePrivacy) Blocked(context.Context, identity.Principal) ([]directory.Person, error) {
	return nil, nil
}

func (fakePrivacy) SetBlocked(context.Context, identity.Principal, string, bool) ([]directory.Person, error) {
	return nil, nil
}

type fakeStories struct{}

func (fakeStories) Publish(_ context.Context, _ identity.Principal, draft story.Draft) (story.Story, error) {
	return story.Story{ID: "s1", ClientID: draft.ClientID, MediaID: draft.MediaID}, nil
}

func (fakeStories) Feed(context.Context, identity.Principal) ([]story.Story, error) {
	return []story.Story{{ID: "s1", MediaID: "m1"}}, nil
}

func (fakeStories) View(_ context.Context, _ identity.Principal, id string) error {
	if id == "missing" {
		return story.ErrNotFound
	}

	return nil
}

func (fakeStories) Delete(context.Context, identity.Principal, string) error {
	return nil
}

func (fakeProfiles) UpdateOwn(_ context.Context, principal identity.Principal, fields profile.Fields) (profile.Profile, error) {
	if fields.Username == "taken" {
		return profile.Profile{}, profile.ErrUsernameTaken
	}

	if fields.Username == "boom" {
		return profile.Profile{}, errors.New("secret database detail")
	}

	return profile.Profile{Subject: principal.Subject, Fields: fields, UpdatedAt: time.Unix(5, 0)}, nil
}

type fakeUpdates struct {
	hub     *updatesusecase.Hub
	mu      sync.Mutex
	journal []update.Update
	broken  bool
}

func (u *fakeUpdates) publish(firstName string) {
	u.mu.Lock()
	seq := uint64(len(u.journal) + 1)
	u.journal = append(u.journal, update.Update{
		Seq:  seq,
		Kind: update.KindProfile,
		Profile: profile.Profile{
			Subject:   "anna",
			Fields:    profile.Fields{FirstName: firstName},
			UpdatedAt: time.Unix(5, 0),
		},
	})
	u.mu.Unlock()

	u.hub.Notify("anna")
}

func (u *fakeUpdates) publishMessage(next message.Message) {
	u.mu.Lock()
	seq := uint64(len(u.journal) + 1)
	u.journal = append(u.journal, update.Update{Seq: seq, Kind: update.KindMessage, Message: next})
	u.mu.Unlock()

	u.hub.Notify("anna")
}

func (u *fakeUpdates) breakReads() {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.broken = true
}

func (u *fakeUpdates) Subscribe(_ context.Context, _ identity.Principal, after uint64) (uint64, error) {
	if after == 403 {
		return 0, identity.ErrForbidden
	}

	if after > update.MaxSeq {
		return 0, update.ErrInvalidSeq
	}

	u.mu.Lock()
	defer u.mu.Unlock()

	return uint64(len(u.journal)), nil
}

func (u *fakeUpdates) After(
	_ context.Context,
	_ identity.Principal,
	after uint64,
	limit int,
) ([]update.Update, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.broken {
		return nil, errors.New("secret database detail")
	}

	if after >= uint64(len(u.journal)) {
		return nil, nil
	}

	page := u.journal[after:]
	if len(page) > limit {
		page = page[:limit]
	}

	return append([]update.Update(nil), page...), nil
}

func (u *fakeUpdates) Listen(subject string) (<-chan struct{}, func()) {
	return u.hub.Listen(subject)
}

type stack struct {
	url     string
	pin     ed25519.PublicKey
	updates *fakeUpdates
}

func start(t *testing.T, expiresIn time.Duration, limits ws.Limits) stack {
	t.Helper()

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	secure, err := wireauth.NewServer(map[uint32]ed25519.PrivateKey{1: private})
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	updates := &fakeUpdates{hub: updatesusecase.NewHub()}
	auth := fakeAuth{expiresIn: expiresIn}
	services := ws.Services{
		Profiles:  fakeProfiles{},
		Updates:   updates,
		Media:     fakeMedia{},
		Stories:   fakeStories{},
		Privacy:   fakePrivacy{},
		Contacts:  fakeContacts{},
		Directory: fakeDirectory{},
		Messaging: fakeMessaging{},
	}
	handler := ws.NewHandler(t.Context(), secure, auth, services, limits, logger)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return stack{url: "ws" + strings.TrimPrefix(server.URL, "http"), pin: public, updates: updates}
}

type clientSocket struct {
	ctx  context.Context
	conn *websocket.Conn
}

func (s clientSocket) ReadMessage() (int, []byte, error) {
	_, payload, err := s.conn.Read(s.ctx)

	return wireauth.BinaryMessageType, payload, err
}

func (s clientSocket) WriteMessage(_ int, payload []byte) error {
	return s.conn.Write(s.ctx, websocket.MessageBinary, payload)
}

type client struct {
	t       *testing.T
	conn    *websocket.Conn
	channel *wireauth.Channel
}

func connect(t *testing.T, target stack) *client {
	t.Helper()

	conn, _, err := websocket.Dial(t.Context(), target.url, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = conn.CloseNow()
	})

	pins := map[uint32]ed25519.PublicKey{1: target.pin}

	channel, err := wireauth.Client(clientSocket{t.Context(), conn}, pins, []uint32{1})
	if err != nil {
		t.Fatal(err)
	}

	return &client{t, conn, channel}
}

func (c *client) send(request codec.Frame) {
	c.t.Helper()

	sealed := c.channel.Seal(codec.EncodeFrame(request))
	if err := c.conn.Write(c.t.Context(), websocket.MessageBinary, sealed); err != nil {
		c.t.Fatal(err)
	}
}

func (c *client) ask(request codec.Frame) codec.Frame {
	c.t.Helper()

	c.send(request)

	return c.receive(request.RequestID)
}

func (c *client) next() codec.Frame {
	c.t.Helper()

	ctx, cancel := context.WithTimeout(c.t.Context(), 5*time.Second)
	defer cancel()

	_, data, err := c.conn.Read(ctx)
	if err != nil {
		c.t.Fatal(err)
	}

	plain, err := c.channel.Open(data)
	if err != nil {
		c.t.Fatal(err)
	}

	frame, err := codec.DecodeFrame(plain)
	if err != nil {
		c.t.Fatal(err)
	}

	return frame
}

func (c *client) receive(requestID uint64) codec.Frame {
	c.t.Helper()

	response := c.next()
	if response.RequestID != requestID {
		c.t.Fatalf("request id %d for %d", response.RequestID, requestID)
	}

	return response
}

func authenticate(id uint64, token string) codec.Frame {
	return codec.Frame{RequestID: id, Kind: codec.KindAuthenticate, Body: codec.EncodeAuthenticate(token)}
}

func signedIn(t *testing.T, target stack) *client {
	t.Helper()

	c := connect(t, target)
	response := c.ask(authenticate(1, goodToken))
	if response.Kind != codec.KindAuthenticated {
		t.Fatalf("got kind %d", response.Kind)
	}

	return c
}

func closeStatus(t *testing.T, c *client) websocket.StatusCode {
	t.Helper()

	_, _, err := c.conn.Read(t.Context())

	return websocket.CloseStatus(err)
}

func TestTheTokenTravelsInsideTheChannel(t *testing.T) {
	t.Parallel()

	target := start(t, time.Minute, ws.DefaultLimits())
	c := connect(t, target)

	response := c.ask(authenticate(1, goodToken))
	expiresAt, err := codec.DecodeAuthenticated(response.Body)
	if response.Kind != codec.KindAuthenticated || err != nil || expiresAt <= time.Now().Unix() {
		t.Fatalf("got %+v, %d, %v", response, expiresAt, err)
	}
}

func TestABadTokenIsAnsweredAndClosed(t *testing.T) {
	t.Parallel()

	target := start(t, time.Minute, ws.DefaultLimits())

	bad := connect(t, target)
	assertError(t, bad.ask(authenticate(1, "bad")), 401, "unauthenticated")
	if status := closeStatus(t, bad); status != ws.StatusTokenExpired {
		t.Fatalf("a bad token: %d", status)
	}

	down := connect(t, target)
	assertError(t, down.ask(authenticate(1, "down")), 503, "unavailable")
	if status := closeStatus(t, down); status != websocket.StatusTryAgainLater {
		t.Fatalf("a check that failed: %d", status)
	}
}

func TestNothingWorksBeforeAuthenticating(t *testing.T) {
	t.Parallel()

	c := connect(t, start(t, time.Minute, ws.DefaultLimits()))
	sealed := c.channel.Seal(codec.EncodeFrame(codec.Frame{RequestID: 1, Kind: codec.KindProfileGet}))
	if err := c.conn.Write(t.Context(), websocket.MessageBinary, sealed); err != nil {
		t.Fatal(err)
	}

	if status := closeStatus(t, c); status != websocket.StatusPolicyViolation {
		t.Fatalf("got %d", status)
	}
}

func TestAPlainConnectionGetsNowhere(t *testing.T) {
	t.Parallel()

	target := start(t, time.Minute, ws.DefaultLimits())
	conn, _, err := websocket.Dial(t.Context(), target.url, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = conn.CloseNow()
	})

	if err := conn.Write(t.Context(), websocket.MessageBinary, []byte("not a hello")); err != nil {
		t.Fatal(err)
	}

	_, _, err = conn.Read(t.Context())
	if status := websocket.CloseStatus(err); status != websocket.StatusPolicyViolation {
		t.Fatalf("got %v", err)
	}
}

func TestPingIsAnswered(t *testing.T) {
	t.Parallel()

	c := signedIn(t, start(t, time.Minute, ws.DefaultLimits()))
	response := c.ask(codec.Frame{RequestID: 2, Kind: codec.KindPing, Body: codec.EncodePing(77)})

	id, err := codec.DecodePong(response.Body)
	if response.Kind != codec.KindPong || err != nil || id != 77 {
		t.Fatalf("got %+v, %d, %v", response, id, err)
	}
}

func TestProfileRequests(t *testing.T) {
	t.Parallel()

	c := signedIn(t, start(t, time.Minute, ws.DefaultLimits()))

	assertError(t, c.ask(codec.Frame{RequestID: 2, Kind: codec.KindProfileGet}), 404, "profile_not_found")

	set := func(id uint64, username string) codec.Frame {
		return codec.Frame{RequestID: id, Kind: codec.KindProfileSet, Body: codec.EncodeProfileSet(codec.ProfileFields{
			FirstName: "Анна", Username: username,
		})}
	}

	response := c.ask(set(3, "anna_s"))
	data, err := codec.DecodeProfileData(response.Body)
	if response.Kind != codec.KindProfileData || err != nil || data.Subject != "anna" || data.Fields.FirstName != "Анна" {
		t.Fatalf("got %+v, %+v, %v", response, data, err)
	}

	assertError(t, c.ask(set(4, "taken")), 409, "username_taken")
	assertError(t, c.ask(set(5, "boom")), 500, "internal")
	assertError(t, c.ask(codec.Frame{RequestID: 6, Kind: codec.Kind(9999)}), 400, "unsupported kind")
}

func assertError(t *testing.T, frame codec.Frame, wantCode int32, wantMessage string) {
	t.Helper()

	code, message, err := codec.DecodeError(frame.Body)
	if frame.Kind != codec.KindError || err != nil || code != wantCode || message != wantMessage {
		t.Fatalf("got kind %d, code %d, %q, %v", frame.Kind, code, message, err)
	}
}

func TestAReplayedPacketEndsTheConnection(t *testing.T) {
	t.Parallel()

	c := signedIn(t, start(t, time.Minute, ws.DefaultLimits()))
	sealed := c.channel.Seal(codec.EncodeFrame(codec.Frame{RequestID: 2, Kind: codec.KindPing, Body: codec.EncodePing(1)}))

	for range 2 {
		if err := c.conn.Write(t.Context(), websocket.MessageBinary, sealed); err != nil {
			t.Fatal(err)
		}
	}

	c.receive(2)
	if status := closeStatus(t, c); status != websocket.StatusPolicyViolation {
		t.Fatalf("got %d", status)
	}
}

func TestTheConnectionEndsWithTheToken(t *testing.T) {
	t.Parallel()

	c := signedIn(t, start(t, 200*time.Millisecond, ws.DefaultLimits()))
	if status := closeStatus(t, c); status != ws.StatusTokenExpired {
		t.Fatalf("got %d", status)
	}
}

func TestAnAddressIsLimited(t *testing.T) {
	t.Parallel()

	limits := ws.DefaultLimits()
	limits.HandshakesPerMinutePerAddress = 1
	target := start(t, time.Minute, limits)
	connect(t, target)

	_, response, err := websocket.Dial(t.Context(), target.url, nil)
	if err == nil || response == nil || response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("got %v, %+v", err, response)
	}

	other := http.Header{"CF-Connecting-IP": {"203.0.113.9"}}
	conn, _, err := websocket.Dial(t.Context(), target.url, &websocket.DialOptions{HTTPHeader: other})
	if err != nil {
		t.Fatalf("another address: %v", err)
	}

	_ = conn.CloseNow()
}

func TestAPersonsConnectionsAreLimited(t *testing.T) {
	t.Parallel()

	limits := ws.DefaultLimits()
	limits.ConnectionsPerSubject = 1
	target := start(t, time.Minute, limits)
	signedIn(t, target)

	second := signedIn(t, target)
	if status := closeStatus(t, second); status != websocket.StatusPolicyViolation {
		t.Fatalf("got %d", status)
	}
}

func TestFloodingEndsTheConnection(t *testing.T) {
	t.Parallel()

	limits := ws.DefaultLimits()
	limits.FramesPerSecond = 0
	limits.FrameBurst = 1
	c := signedIn(t, start(t, time.Minute, limits))

	c.ask(codec.Frame{RequestID: 2, Kind: codec.KindPing, Body: codec.EncodePing(1)})

	sealed := c.channel.Seal(codec.EncodeFrame(codec.Frame{RequestID: 3, Kind: codec.KindPing, Body: codec.EncodePing(2)}))
	if err := c.conn.Write(t.Context(), websocket.MessageBinary, sealed); err != nil {
		t.Fatal(err)
	}

	if status := closeStatus(t, c); status != websocket.StatusPolicyViolation {
		t.Fatalf("got %d", status)
	}
}
