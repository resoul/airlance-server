package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	domain "go.airlance.org/server/internal/domain/message"
)

type entityRow struct {
	Kind   int32  `json:"kind"`
	Offset int32  `json:"offset"`
	Length int32  `json:"length"`
	UserID string `json:"user_id,omitempty"`
}

func encodeEntities(entities []domain.Entity) ([]byte, error) {
	rows := make([]entityRow, len(entities))
	for index, next := range entities {
		rows[index] = entityRow{Kind: int32(next.Kind), Offset: next.Offset, Length: next.Length, UserID: next.UserID}
	}

	encoded, err := json.Marshal(rows)
	if err != nil {
		return nil, fmt.Errorf("encode entities: %w", err)
	}

	return encoded, nil
}

func decodeEntities(raw []byte) ([]domain.Entity, error) {
	var rows []entityRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("decode entities: %w", err)
	}

	if len(rows) == 0 {
		return nil, nil
	}

	entities := make([]domain.Entity, len(rows))
	for index, next := range rows {
		entities[index] = domain.Entity{
			Kind: domain.EntityKind(next.Kind), Offset: next.Offset, Length: next.Length, UserID: next.UserID,
		}
	}

	return entities, nil
}

func checkReply(ctx context.Context, tx pgx.Tx, chatID string, replyToID int64) error {
	if replyToID == 0 {
		return nil
	}

	var found bool

	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM messages WHERE chat_id = $1 AND message_id = $2 AND deleted_at IS NULL
		)`, chatID, replyToID,
	).Scan(&found)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("check reply: %w", err)
	}

	if !found {
		return domain.ErrInvalidReply
	}

	return nil
}
