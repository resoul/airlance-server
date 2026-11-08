package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/update"
)

func (r *Messages) SetArchived(ctx context.Context, chatID, subject string, isArchived bool) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE chat_members SET is_archived = $3
			WHERE chat_id = $1 AND subject = $2 AND is_archived <> $3`, chatID, subject, isArchived,
		)
		if err != nil {
			return fmt.Errorf("update archive: %w", err)
		}

		if tag.RowsAffected() == 0 {
			return ensureMember(ctx, tx, chatID, subject)
		}

		return appendUpdate(ctx, tx, subject, update.KindChats)
	})
	if err != nil {
		return fmt.Errorf("set archive: %w", err)
	}

	return nil
}

func ensureMember(ctx context.Context, tx pgx.Tx, chatID, subject string) error {
	var found bool

	err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM chat_members WHERE chat_id = $1 AND subject = $2)`, chatID, subject,
	).Scan(&found)
	if err != nil {
		return fmt.Errorf("check member: %w", err)
	}

	if !found {
		return domain.ErrChatNotFound
	}

	return nil
}

func (r *Messages) SetDraft(
	ctx context.Context,
	chatID, subject, text string,
	now time.Time,
) (domain.DraftState, error) {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if err := ensureMember(ctx, tx, chatID, subject); err != nil {
			return err
		}

		changed, err := writeDraft(ctx, tx, chatID, subject, text, now)
		if err != nil || !changed {
			return err
		}

		_, err = tx.Exec(ctx, `
			WITH head AS (
				INSERT INTO update_heads (subject, seq) VALUES ($1, 1)
				ON CONFLICT (subject) DO UPDATE SET seq = update_heads.seq + 1
				RETURNING seq
			)
			INSERT INTO updates (subject, seq, kind, chat_id) SELECT $1, seq, $2, $3 FROM head`,
			subject, int16(update.KindDraft), chatID,
		)
		if err != nil {
			return fmt.Errorf("append draft update: %w", err)
		}

		return nil
	})
	if err != nil {
		return domain.DraftState{}, fmt.Errorf("set draft: %w", err)
	}

	return draftState(ctx, r.pool, subject, chatID)
}

func writeDraft(ctx context.Context, tx pgx.Tx, chatID, subject, text string, now time.Time) (bool, error) {
	if text == "" {
		tag, err := tx.Exec(ctx, `DELETE FROM chat_drafts WHERE chat_id = $1 AND subject = $2`, chatID, subject)
		if err != nil {
			return false, fmt.Errorf("delete draft: %w", err)
		}

		return tag.RowsAffected() == 1, nil
	}

	tag, err := tx.Exec(ctx, `
		INSERT INTO chat_drafts (chat_id, subject, text, updated_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT (chat_id, subject) DO UPDATE SET text = excluded.text, updated_at = excluded.updated_at
		WHERE chat_drafts.text <> excluded.text`, chatID, subject, text, now,
	)
	if err != nil {
		return false, fmt.Errorf("upsert draft: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

func draftState(ctx context.Context, pool *pgxpool.Pool, subject, chatID string) (domain.DraftState, error) {
	state := domain.DraftState{ChatID: chatID}

	err := pool.QueryRow(ctx, `
		SELECT text, (extract(epoch FROM updated_at) * 1000)::bigint FROM chat_drafts
		WHERE chat_id = $1 AND subject = $2`, chatID, subject,
	).Scan(&state.Text, &state.UpdatedAtMs)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, nil
	}

	if err != nil {
		return domain.DraftState{}, fmt.Errorf("select draft: %w", err)
	}

	return state, nil
}
