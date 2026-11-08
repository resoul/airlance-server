package postgres

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	domain "go.airlance.org/server/internal/domain/message"
)

func checkThread(ctx context.Context, tx pgx.Tx, chatID string, thread int64) error {
	if thread == 0 {
		return nil
	}

	var found bool

	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM messages m JOIN chats c ON c.id = m.chat_id
			WHERE m.chat_id = $1 AND m.message_id = $2 AND m.thread_id = 0 AND m.deleted_at IS NULL AND c.kind = $3
		)`, chatID, thread, int16(domain.KindChannel),
	).Scan(&found)
	if err != nil {
		return fmt.Errorf("check thread: %w", err)
	}

	if !found {
		return domain.ErrInvalidThread
	}

	return nil
}

func audience(ctx context.Context, tx pgx.Tx, chatID string, thread int64, members []string) ([]string, error) {
	if thread == 0 {
		return members, nil
	}

	rows, err := tx.Query(ctx, `
		SELECT DISTINCT sender FROM messages
		WHERE chat_id = $1 AND (thread_id = $2 OR message_id = $2)
		ORDER BY sender`, chatID, thread,
	)
	if err != nil {
		return nil, fmt.Errorf("select thread authors: %w", err)
	}

	authors, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("read thread authors: %w", err)
	}

	listeners := make([]string, 0, len(authors))

	for _, author := range authors {
		if slices.Contains(members, author) {
			listeners = append(listeners, author)
		}
	}

	return listeners, nil
}
