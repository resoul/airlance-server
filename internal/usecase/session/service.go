package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.airlance.org/server/internal/domain/identity"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrUnavailable  = errors.New("token check unavailable")
)

type Token struct {
	Subject   string
	ClientID  string
	Scopes    []string
	ExpiresAt time.Time
}

type Verifier interface {
	Verify(ctx context.Context, token string) (Token, error)
}

type Service struct {
	verifier Verifier
}

func New(verifier Verifier) *Service {
	return &Service{verifier: verifier}
}

func (s *Service) Authenticate(ctx context.Context, token string) (identity.Principal, error) {
	verified, err := s.verifier.Verify(ctx, token)
	if err != nil {
		return identity.Principal{}, fmt.Errorf("verify token: %w", err)
	}

	if verified.Subject == verified.ClientID {
		return identity.Principal{}, fmt.Errorf("a service identity: %w", ErrInvalidToken)
	}

	return identity.Principal{Subject: verified.Subject, Scopes: verified.Scopes, ExpiresAt: verified.ExpiresAt}, nil
}
