package oidc

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"go.airlance.org/server/internal/usecase/session"
)

const (
	tokenType     = "at+jwt"
	maxTokenBytes = 16 << 10
	maxKeysBytes  = 1 << 20
	leeway        = 30 * time.Second
	keysMaxAge    = time.Hour
	minRefetch    = 30 * time.Second
	fetchTimeout  = 10 * time.Second
	userAgent     = "airlance-server"
)

type Config struct {
	Issuer   string
	Audience string
	JWKSURL  string
	Client   *http.Client
	Now      func() time.Time
}

type Verifier struct {
	cfg       Config
	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
	triedAt   time.Time
}

func New(cfg Config) *Verifier {
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: fetchTimeout}
	}

	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	return &Verifier{cfg: cfg}
}

func (v *Verifier) Verify(ctx context.Context, token string) (session.Token, error) {
	if len(token) > maxTokenBytes || strings.Count(token, ".") != 2 || !safeHeader(token) {
		return session.Token{}, invalid("not a compact token with a plain header")
	}

	parsed, err := jwt.ParseSigned(token, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil || len(parsed.Headers) != 1 {
		return session.Token{}, invalid("not one RS256 signature")
	}

	header := parsed.Headers[0]
	if typ, _ := header.ExtraHeaders[jose.HeaderType].(string); typ != tokenType || header.KeyID == "" {
		return session.Token{}, invalid("not an access token")
	}

	key, err := v.key(ctx, header.KeyID)
	if err != nil {
		return session.Token{}, err
	}

	var (
		std     jwt.Claims
		private struct {
			ClientID string `json:"client_id"`
			Scope    string `json:"scope"`
		}
	)
	if err := parsed.Claims(key, &std, &private); err != nil {
		return session.Token{}, invalid("signature")
	}

	return v.check(std, private.ClientID, private.Scope)
}

func (v *Verifier) check(std jwt.Claims, clientID, scope string) (session.Token, error) {
	if len(std.Audience) != 1 {
		return session.Token{}, invalid("not exactly one audience")
	}

	expected := jwt.Expected{Issuer: v.cfg.Issuer, AnyAudience: jwt.Audience{v.cfg.Audience}, Time: v.cfg.Now()}
	if err := std.ValidateWithLeeway(expected, leeway); err != nil {
		return session.Token{}, invalid("issuer, audience or time")
	}

	if std.Subject == "" || std.Expiry == nil || clientID == "" {
		return session.Token{}, invalid("a required claim is missing")
	}

	return session.Token{
		Subject:   std.Subject,
		ClientID:  clientID,
		Scopes:    strings.Fields(scope),
		ExpiresAt: std.Expiry.Time(),
	}, nil
}

func safeHeader(token string) bool {
	head, _, _ := strings.Cut(token, ".")

	raw, err := base64.RawURLEncoding.DecodeString(head)
	if err != nil {
		return false
	}

	var names map[string]json.RawMessage
	if err := json.Unmarshal(raw, &names); err != nil {
		return false
	}

	for _, name := range []string{"jku", "jwk", "x5u", "x5c"} {
		if _, found := names[name]; found {
			return false
		}
	}

	return true
}

func (v *Verifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	now := v.cfg.Now()
	key, known := v.keys[kid]
	if known && now.Sub(v.fetchedAt) < keysMaxAge {
		return key, nil
	}

	if now.Sub(v.triedAt) >= minRefetch || v.triedAt.IsZero() {
		v.triedAt = now
		if err := v.fetch(ctx, now); err != nil {
			if known {
				return key, nil
			}

			return nil, fmt.Errorf("%w: %w", session.ErrUnavailable, err)
		}
	}

	key, known = v.keys[kid]
	if !known {
		if v.keys == nil {
			return nil, session.ErrUnavailable
		}

		return nil, invalid("unknown key")
	}

	return key, nil
}

func (v *Verifier) fetch(ctx context.Context, now time.Time) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, v.cfg.JWKSURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	request.Header.Set("User-Agent", userAgent)

	response, err := v.cfg.Client.Do(request)
	if err != nil {
		return fmt.Errorf("fetch keys: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch keys: HTTP %d", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxKeysBytes))
	if err != nil {
		return fmt.Errorf("read keys: %w", err)
	}

	var set jose.JSONWebKeySet
	if err := json.Unmarshal(body, &set); err != nil {
		return fmt.Errorf("parse keys: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, candidate := range set.Keys {
		if key, ok := candidate.Key.(*rsa.PublicKey); ok && candidate.KeyID != "" {
			keys[candidate.KeyID] = key
		}
	}

	if len(keys) == 0 {
		return errors.New("no usable keys")
	}

	v.keys = keys
	v.fetchedAt = now

	return nil
}

func invalid(reason string) error {
	return fmt.Errorf("%w: %s", session.ErrInvalidToken, reason)
}
