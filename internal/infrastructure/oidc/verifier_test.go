package oidc_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"go.airlance.org/server/internal/infrastructure/oidc"
	"go.airlance.org/server/internal/usecase/session"
)

const (
	issuer   = "https://auth.example.test"
	audience = "messenger-api"
)

var testNow = time.Unix(1_800_000_000, 0)

type options struct {
	key      *rsa.PrivateKey
	kid      string
	typ      string
	issuer   string
	audience []string
	expiry   time.Time
}

func sign(t *testing.T, opts options) string {
	t.Helper()

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: opts.key, KeyID: opts.kid}},
		(&jose.SignerOptions{}).WithHeader(jose.HeaderType, opts.typ),
	)
	if err != nil {
		t.Fatal(err)
	}

	claims := jwt.Claims{
		Issuer:   opts.issuer,
		Subject:  "person",
		Audience: opts.audience,
		Expiry:   jwt.NewNumericDate(opts.expiry),
		IssuedAt: jwt.NewNumericDate(testNow),
	}
	private := map[string]any{"client_id": "airlance-ios", "scope": "profile:read profile:write"}

	token, err := jwt.Signed(signer).Claims(claims).Claims(private).Serialize()
	if err != nil {
		t.Fatal(err)
	}

	return token
}

func setup(t *testing.T) (*oidc.Verifier, options, *atomic.Int32) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	fetches := &atomic.Int32{}
	keys := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "a", Algorithm: "RS256", Use: "sig"}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		if err := json.NewEncoder(w).Encode(keys); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)

	verifier := oidc.New(oidc.Config{
		Issuer:   issuer,
		Audience: audience,
		JWKSURL:  server.URL,
		Client:   server.Client(),
		Now: func() time.Time {
			return testNow
		},
	})
	good := options{
		key: key, kid: "a", typ: "at+jwt", issuer: issuer, audience: []string{audience}, expiry: testNow.Add(5 * time.Minute),
	}

	return verifier, good, fetches
}

func TestAGoodTokenSaysWhoAndWhat(t *testing.T) {
	t.Parallel()

	verifier, good, fetches := setup(t)

	got, err := verifier.Verify(context.Background(), sign(t, good))
	if err != nil {
		t.Fatal(err)
	}

	if got.Subject != "person" || got.ClientID != "airlance-ios" || len(got.Scopes) != 2 {
		t.Fatalf("got %+v", got)
	}

	if _, err := verifier.Verify(context.Background(), sign(t, good)); err != nil {
		t.Fatal(err)
	}

	if fetches.Load() != 1 {
		t.Fatalf("the keys were fetched %d times", fetches.Load())
	}
}

func TestBadTokensAreRefused(t *testing.T) {
	t.Parallel()

	verifier, good, _ := setup(t)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]func(*options){
		"signed by another key": func(o *options) { o.key = otherKey },
		"an unknown key id":     func(o *options) { o.kid = "b" },
		"an ID token":           func(o *options) { o.typ = "JWT" },
		"another issuer":        func(o *options) { o.issuer = "https://evil.example.test" },
		"another API":           func(o *options) { o.audience = []string{"video-api"} },
		"two audiences":         func(o *options) { o.audience = []string{audience, "video-api"} },
		"expired":               func(o *options) { o.expiry = testNow.Add(-time.Hour) },
	}
	for name, change := range cases {
		opts := good
		change(&opts)

		if _, err := verifier.Verify(context.Background(), sign(t, opts)); !errors.Is(err, session.ErrInvalidToken) {
			t.Errorf("%s: %v", name, err)
		}
	}

	for _, garbage := range []string{"", "a.b", "a.b.c", strings.Repeat("a", 20_000) + ".b.c"} {
		if _, err := verifier.Verify(context.Background(), garbage); !errors.Is(err, session.ErrInvalidToken) {
			t.Errorf("%q: %v", garbage[:min(len(garbage), 10)], err)
		}
	}
}

func TestAProviderOutageIsNotTheClientsFault(t *testing.T) {
	t.Parallel()

	_, good, _ := setup(t)
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(down.Close)

	verifier := oidc.New(oidc.Config{
		Issuer: issuer, Audience: audience, JWKSURL: down.URL, Client: down.Client(),
		Now: func() time.Time {
			return testNow
		},
	})
	if _, err := verifier.Verify(context.Background(), sign(t, good)); !errors.Is(err, session.ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
}
