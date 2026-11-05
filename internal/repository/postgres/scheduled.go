package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "go.airlance.org/server/internal/domain/scheduled"
	"go.airlance.org/server/internal/domain/update"
)

const scheduledColumns = `id, chat_id, sender, client_id, text, send_at, repeat_rule, timezone, failure`

type Scheduled struct {
	pool *pgxpool.Pool
}

func NewScheduled(pool *pgxpool.Pool) *Scheduled {
	return &Scheduled{pool: pool}
}

func (r *Scheduled) Add(ctx context.Context, item domain.Item, now time.Time) (domain.Item, error) {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO scheduled_messages (
				id, chat_id, sender, client_id, text, send_at, repeat_rule, timezone, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (sender, client_id) DO NOTHING`,
			item.ID, item.ChatID, item.Sender, item.ClientID, item.Text, item.SendAt, item.Repeat, item.Timezone, now,
		)
		if err != nil {
			return fmt.Errorf("insert scheduled message: %w", err)
		}

		return appendUpdate(ctx, tx, item.Sender, update.KindScheduled)
	})
	if err != nil {
		return domain.Item{}, fmt.Errorf("add scheduled message: %w", err)
	}

	return item, nil
}

func (r *Scheduled) CountPending(ctx context.Context, chatID, sender string) (int, error) {
	var count int

	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM scheduled_messages WHERE chat_id = $1 AND sender = $2`, chatID, sender,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count scheduled messages: %w", err)
	}

	return count, nil
}

func (r *Scheduled) List(ctx context.Context, sender string) ([]domain.Item, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+scheduledColumns+` FROM scheduled_messages WHERE sender = $1 ORDER BY send_at, id`, sender,
	)
	if err != nil {
		return nil, fmt.Errorf("select scheduled messages: %w", err)
	}

	return collectScheduled(rows)
}

func collectScheduled(rows pgx.Rows) ([]domain.Item, error) {
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Item, error) {
		var next domain.Item

		err := row.Scan(
			&next.ID, &next.ChatID, &next.Sender, &next.ClientID, &next.Text, &next.SendAt,
			&next.Repeat, &next.Timezone, &next.Failure,
		)

		return next, err
	})
	if err != nil {
		return nil, fmt.Errorf("read scheduled messages: %w", err)
	}

	return items, nil
}

func (r *Scheduled) change(ctx context.Context, sender, query string, args ...any) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("change scheduled message: %w", err)
		}

		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}

		return appendUpdate(ctx, tx, sender, update.KindScheduled)
	})
	if err != nil {
		return fmt.Errorf("scheduled message: %w", err)
	}

	return nil
}

func (r *Scheduled) Edit(ctx context.Context, id, sender, text string, sendAt time.Time) error {
	return r.change(ctx, sender, `
		UPDATE scheduled_messages SET text = $3, send_at = $4, failure = '', leased_until = NULL
		WHERE id = $1 AND sender = $2`, id, sender, text, sendAt)
}

func (r *Scheduled) Cancel(ctx context.Context, id, sender string) error {
	return r.change(ctx, sender, `DELETE FROM scheduled_messages WHERE id = $1 AND sender = $2`, id, sender)
}

func (r *Scheduled) SendNow(ctx context.Context, id, sender string, now time.Time) error {
	return r.change(ctx, sender, `
		UPDATE scheduled_messages SET send_at = $3, failure = '', leased_until = NULL
		WHERE id = $1 AND sender = $2`, id, sender, now)
}

func (r *Scheduled) Claim(ctx context.Context, now, leasedUntil time.Time, limit int) ([]domain.Item, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE scheduled_messages SET leased_until = $2
		WHERE id IN (
			SELECT id FROM scheduled_messages
			WHERE failure = '' AND send_at <= $1 AND (leased_until IS NULL OR leased_until < $1)
			ORDER BY send_at
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		)
		RETURNING `+scheduledColumns, now, leasedUntil, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("claim scheduled messages: %w", err)
	}

	return collectScheduled(rows)
}

func (r *Scheduled) Done(ctx context.Context, item domain.Item) error {
	return r.change(ctx, item.Sender, `DELETE FROM scheduled_messages WHERE id = $1`, item.ID)
}

func (r *Scheduled) Reschedule(ctx context.Context, item domain.Item, next time.Time) error {
	return r.change(ctx, item.Sender, `
		UPDATE scheduled_messages SET send_at = $2, leased_until = NULL WHERE id = $1`, item.ID, next)
}

func (r *Scheduled) Fail(ctx context.Context, item domain.Item, reason string) error {
	return r.change(ctx, item.Sender, `
		UPDATE scheduled_messages SET failure = $2, leased_until = NULL WHERE id = $1`, item.ID, reason)
}
