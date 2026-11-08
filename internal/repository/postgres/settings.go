package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "go.airlance.org/server/internal/domain/settings"
	"go.airlance.org/server/internal/domain/update"
)

type Settings struct {
	pool *pgxpool.Pool
}

func NewSettings(pool *pgxpool.Pool) *Settings {
	return &Settings{pool: pool}
}

func (r *Settings) Get(ctx context.Context, subject, key string) (domain.Record, error) {
	return readSettings(ctx, r.pool, subject, key)
}

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func readSettings(ctx context.Context, db rowQuerier, subject, key string) (domain.Record, error) {
	record := domain.Record{Key: key}

	err := db.QueryRow(ctx, `
		SELECT version, data, updated_at FROM user_settings WHERE subject = $1 AND key = $2`, subject, key,
	).Scan(&record.Version, &record.Data, &record.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return record, nil
	}

	if err != nil {
		return domain.Record{}, fmt.Errorf("select settings: %w", err)
	}

	return record, nil
}

func (r *Settings) Put(
	ctx context.Context,
	subject, key string,
	data []byte,
	now time.Time,
) (domain.Record, error) {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO user_settings (subject, key, version, data, updated_at) VALUES ($1, $2, 1, $3, $4)
			ON CONFLICT (subject, key) DO UPDATE
			SET version = user_settings.version + 1, data = excluded.data, updated_at = excluded.updated_at`,
			subject, key, data, now,
		)
		if err != nil {
			return fmt.Errorf("upsert settings: %w", err)
		}

		_, err = tx.Exec(ctx, `
			WITH head AS (
				INSERT INTO update_heads (subject, seq) VALUES ($1, 1)
				ON CONFLICT (subject) DO UPDATE SET seq = update_heads.seq + 1
				RETURNING seq
			)
			INSERT INTO updates (subject, seq, kind, chat_id) SELECT $1, seq, $2, $3 FROM head`,
			subject, int16(update.KindSettings), key,
		)
		if err != nil {
			return fmt.Errorf("append settings update: %w", err)
		}

		return nil
	})
	if err != nil {
		return domain.Record{}, fmt.Errorf("put settings: %w", err)
	}

	return readSettings(ctx, r.pool, subject, key)
}

func (r *Settings) Wallpapers(ctx context.Context) ([]domain.Wallpaper, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name, media_id, sort FROM wallpapers ORDER BY sort, name, id`)
	if err != nil {
		return nil, fmt.Errorf("select wallpapers: %w", err)
	}

	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Wallpaper, error) {
		var next domain.Wallpaper

		return next, row.Scan(&next.ID, &next.Name, &next.MediaID, &next.Sort)
	})
	if err != nil {
		return nil, fmt.Errorf("read wallpapers: %w", err)
	}

	return list, nil
}

func (r *Settings) AddWallpaper(ctx context.Context, next domain.Wallpaper, now time.Time) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO wallpapers (id, name, media_id, sort, created_at) VALUES ($1, $2, $3, $4, $5)`,
		next.ID, next.Name, next.MediaID, next.Sort, now,
	)
	if err != nil {
		return fmt.Errorf("insert wallpaper: %w", err)
	}

	return nil
}

func (r *Settings) RemoveWallpaper(ctx context.Context, id string) (string, error) {
	var mediaID string

	err := r.pool.QueryRow(ctx, `DELETE FROM wallpapers WHERE id = $1 RETURNING media_id`, id).Scan(&mediaID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}

	if err != nil {
		return "", fmt.Errorf("delete wallpaper: %w", err)
	}

	return mediaID, nil
}
