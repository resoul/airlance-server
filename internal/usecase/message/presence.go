package message

import (
	"context"
	"fmt"

	"go.airlance.org/server/internal/domain/identity"
	domain "go.airlance.org/server/internal/domain/message"
)

func (s *Service) Readers(
	ctx context.Context,
	principal identity.Principal,
	chatID string,
	messageID int64,
) ([]domain.Member, error) {
	if !principal.HasScope(domain.ScopeRead) {
		return nil, identity.ErrForbidden
	}

	readers, err := s.repository.Readers(ctx, chatID, principal.Subject, messageID, domain.MaxReadersPage)
	if err != nil {
		return nil, fmt.Errorf("read readers: %w", err)
	}

	return readers, nil
}

func (s *Service) Typing(
	ctx context.Context,
	principal identity.Principal,
	chatID string,
) (domain.Peer, []string, error) {
	if !principal.HasScope(domain.ScopeWrite) {
		return domain.Peer{}, nil, identity.ErrForbidden
	}

	who, audience, err := s.repository.TypingAudience(ctx, chatID, principal.Subject)
	if err != nil {
		return domain.Peer{}, nil, fmt.Errorf("find audience: %w", err)
	}

	return who, audience, nil
}
