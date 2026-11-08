package message

import (
	"context"
	"fmt"

	"go.airlance.org/server/internal/domain/identity"
	domain "go.airlance.org/server/internal/domain/message"
)

func (s *Service) ChatLog(
	ctx context.Context,
	principal identity.Principal,
	chatID string,
) ([]domain.LogEntry, error) {
	if !principal.HasScope(domain.ScopeRead) {
		return nil, identity.ErrForbidden
	}

	entries, err := s.repository.ChatLog(ctx, chatID, principal.Subject, domain.MaxLogPage)
	if err != nil {
		return nil, fmt.Errorf("read log: %w", err)
	}

	return entries, nil
}
