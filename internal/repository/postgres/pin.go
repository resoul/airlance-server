package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/update"
)

func (r *Messages) Pins(ctx context.Context, chatID, subject string) ([]int64, error) {
	var mayRead bool

	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM chat_members WHERE chat_id = $1 AND subject = $2)
			OR EXISTS (SELECT 1 FROM chats WHERE id = $1 AND link IS NOT NULL)`, chatID, subject,
	).Scan(&mayRead)
	if err != nil {
		return nil, fmt.Errorf("check member: %w", err)
	}

	if !mayRead {
		return nil, domain.ErrChatNotFound
	}

	return pinsOf(ctx, r.pool, chatID)
}

func pinsOf(ctx context.Context, db querier, chatID string) ([]int64, error) {
	rows, err := db.Query(ctx, `SELECT message_id FROM chat_pins WHERE chat_id = $1 ORDER BY message_id`, chatID)
	if err != nil {
		return nil, fmt.Errorf("select pins: %w", err)
	}

	pins, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, fmt.Errorf("read pins: %w", err)
	}

	return pins, nil
}

func (r *Messages) SetPin(
	ctx context.Context,
	chatID string,
	id int64,
	subject string,
	isPinned bool,
	now time.Time,
) ([]int64, []string, error) {
	var (
		pins    []int64
		members []string
	)

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var err error

		members, err = membersOf(ctx, tx, chatID)
		if err != nil {
			return err
		}

		if !slices.Contains(members, subject) {
			return domain.ErrChatNotFound
		}

		if err := checkMayPin(ctx, tx, chatID, subject); err != nil {
			return err
		}

		changed, err := changePin(ctx, tx, chatID, id, isPinned, now)
		if err != nil {
			return err
		}

		pins, err = pinsOf(ctx, tx, chatID)
		if err != nil || !changed {
			return err
		}

		if len(pins) > domain.MaxPins {
			return domain.ErrTooManyPins
		}

		return announcePins(ctx, tx, members, chatID)
	})
	if err != nil {
		return nil, nil, fmt.Errorf("set pin: %w", err)
	}

	return pins, members, nil
}

func checkMayPin(ctx context.Context, tx pgx.Tx, chatID, subject string) error {
	access, err := requireAccess(ctx, tx, chatID, subject)
	if err != nil {
		return err
	}

	if !domain.MayPin(access) {
		return domain.ErrNotAllowed
	}

	return nil
}

func changePin(ctx context.Context, tx pgx.Tx, chatID string, id int64, isPinned bool, now time.Time) (bool, error) {
	if !isPinned {
		tag, err := tx.Exec(ctx, `DELETE FROM chat_pins WHERE chat_id = $1 AND message_id = $2`, chatID, id)
		if err != nil {
			return false, fmt.Errorf("delete pin: %w", err)
		}

		return tag.RowsAffected() == 1, nil
	}

	var found bool

	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM messages WHERE chat_id = $1 AND message_id = $2 AND thread_id = 0 AND deleted_at IS NULL
		)`, chatID, id,
	).Scan(&found)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("check message: %w", err)
	}

	if !found {
		return false, domain.ErrMessageMissing
	}

	tag, err := tx.Exec(ctx, `
		INSERT INTO chat_pins (chat_id, message_id, pinned_at) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		chatID, id, now,
	)
	if err != nil {
		return false, fmt.Errorf("insert pin: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

func announcePins(ctx context.Context, tx pgx.Tx, members []string, chatID string) error {
	for _, member := range members {
		_, err := tx.Exec(ctx, `
			WITH head AS (
				INSERT INTO update_heads (subject, seq) VALUES ($1, 1)
				ON CONFLICT (subject) DO UPDATE SET seq = update_heads.seq + 1
				RETURNING seq
			)
			INSERT INTO updates (subject, seq, kind, chat_id) SELECT $1, seq, $2, $3 FROM head`,
			member, int16(update.KindPins), chatID,
		)
		if err != nil {
			return fmt.Errorf("append pins update: %w", err)
		}
	}

	return nil
}

func unpinRemoved(ctx context.Context, tx pgx.Tx, chatID string, ids []int64, members []string) error {
	tag, err := tx.Exec(ctx, `DELETE FROM chat_pins WHERE chat_id = $1 AND message_id = ANY($2)`, chatID, ids)
	if err != nil {
		return fmt.Errorf("unpin removed: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return nil
	}

	return announcePins(ctx, tx, members, chatID)
}

func pinsFor(ctx context.Context, pool *pgxpool.Pool, chatIDs []string) (map[string][]int64, error) {
	found := make(map[string][]int64, len(chatIDs))

	for _, chatID := range chatIDs {
		pins, err := pinsOf(ctx, pool, chatID)
		if err != nil {
			return nil, err
		}

		found[chatID] = pins
	}

	return found, nil
}
