package ws_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/profile"
	"go.airlance.org/server/internal/transport/codec"
	"go.airlance.org/server/internal/transport/ws"
	"go.airlance.org/server/internal/usecase/session"
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

func (fakeProfiles) UpdateOwn(_ context.Context, principal identity.Principal, fields profile.Fields) (profile.Profile, error) {
	if fields.Username == "taken" {
		return profile.Profile{}, profile.ErrUsernameTaken
	}

	if fields.Username == "boom" {
		return profile.Profile{}, errors.New("secret database detail")
	}

	return profile.Profile{Subject: principal.Subject, Fields: fields, UpdatedAt: time.Unix(5, 0)}, nil
}

func start(t *testing.T, expiresIn time.Duration) string {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := ws.NewHandler(t.Context(), fakeAuth{expiresIn: expiresIn}, fakeProfiles{}, logger)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return "ws" + strings.TrimPrefix(server.URL, "http")
}

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()

	conn, _, err := websocket.Dial(t.Context(), url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + goodToken}},
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = conn.CloseNow()
	})

	return conn
}

func ask(t *testing.T, conn *websocket.Conn, request codec.Frame) codec.Frame {
	t.Helper()

	if err := conn.Write(t.Context(), websocket.MessageBinary, codec.EncodeFrame(request)); err != nil {
		t.Fatal(err)
	}

	_, data, err := conn.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	response, err := codec.DecodeFrame(data)
	if err != nil {
		t.Fatal(err)
	}

	if response.RequestID != request.RequestID {
		t.Fatalf("request id %d for %d", response.RequestID, request.RequestID)
	}

	return response
}

func TestATokenIsCheckedBeforeTheConnectionOpens(t *testing.T) {
	t.Parallel()

	url := start(t, time.Minute)
	for token, want := range map[string]int{"": 401, "bad": 401, "down": 503} {
		header := http.Header{}
		if token != "" {
			header.Set("Authorization", "Bearer "+token)
		}

		_, response, err := websocket.Dial(t.Context(), url, &websocket.DialOptions{HTTPHeader: header})
		if err == nil {
			t.Fatalf("%q: connected", token)
		}

		if response.StatusCode != want {
			t.Errorf("%q: HTTP %d, want %d", token, response.StatusCode, want)
		}
	}
}

func TestPingIsAnswered(t *testing.T) {
	t.Parallel()

	conn := dial(t, start(t, time.Minute))
	response := ask(t, conn, codec.Frame{RequestID: 1, Kind: codec.KindPing, Body: codec.EncodePing(77)})

	id, err := codec.DecodePong(response.Body)
	if response.Kind != codec.KindPong || err != nil || id != 77 {
		t.Fatalf("got %+v, %d, %v", response, id, err)
	}
}

func TestProfileRequests(t *testing.T) {
	t.Parallel()

	conn := dial(t, start(t, time.Minute))

	assertError(t, ask(t, conn, codec.Frame{RequestID: 1, Kind: codec.KindProfileGet}), 404, "profile_not_found")

	set := func(id uint64, username string) codec.Frame {
		return codec.Frame{RequestID: id, Kind: codec.KindProfileSet, Body: codec.EncodeProfileSet(codec.ProfileFields{
			FirstName: "Анна", Username: username,
		})}
	}

	response := ask(t, conn, set(2, "anna_s"))
	data, err := codec.DecodeProfileData(response.Body)
	if response.Kind != codec.KindProfileData || err != nil || data.Subject != "anna" || data.Fields.FirstName != "Анна" {
		t.Fatalf("got %+v, %+v, %v", response, data, err)
	}

	assertError(t, ask(t, conn, set(3, "taken")), 409, "username_taken")
	assertError(t, ask(t, conn, set(4, "boom")), 500, "internal")
	assertError(t, ask(t, conn, codec.Frame{RequestID: 5, Kind: codec.Kind(9999)}), 400, "unsupported kind")
}

func assertError(t *testing.T, frame codec.Frame, wantCode int32, wantMessage string) {
	t.Helper()

	code, message, err := codec.DecodeError(frame.Body)
	if frame.Kind != codec.KindError || err != nil || code != wantCode || message != wantMessage {
		t.Fatalf("got kind %d, code %d, %q, %v", frame.Kind, code, message, err)
	}
}

func TestABrokenFrameEndsTheConnection(t *testing.T) {
	t.Parallel()

	conn := dial(t, start(t, time.Minute))
	if err := conn.Write(t.Context(), websocket.MessageBinary, []byte("not a frame")); err != nil {
		t.Fatal(err)
	}

	_, _, err := conn.Read(t.Context())
	if status := websocket.CloseStatus(err); status != websocket.StatusInvalidFramePayloadData {
		t.Fatalf("got %v", err)
	}
}

func TestTheConnectionEndsWithTheToken(t *testing.T) {
	t.Parallel()

	conn := dial(t, start(t, 100*time.Millisecond))

	_, _, err := conn.Read(t.Context())
	if status := websocket.CloseStatus(err); status != ws.StatusTokenExpired {
		t.Fatalf("got %v", err)
	}
}
