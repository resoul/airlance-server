package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "go.airlance.org/server/internal/domain/linkpreview"
)

type LinkPreviews struct {
	pool *pgxpool.Pool
}

func NewLinkPreviews(pool *pgxpool.Pool) *LinkPreviews {
	return &LinkPreviews{pool: pool}
}

func (r *LinkPreviews) Get(ctx context.Context, link string) (domain.Preview, time.Time, bool, error) {
	var (
		preview domain.Preview
		at      time.Time
	)

	err := r.pool.QueryRow(ctx, `
		SELECT url, found, site, title, summary, photo_media_id, width, height, fetched_at
		FROM link_previews WHERE url = $1`, link,
	).Scan(&preview.URL, &preview.Found, &preview.Site, &preview.Title, &preview.Summary,
		&preview.PhotoMediaID, &preview.Width, &preview.Height, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Preview{}, time.Time{}, false, nil
	}

	if err != nil {
		return domain.Preview{}, time.Time{}, false, fmt.Errorf("select link preview: %w", err)
	}

	return preview, at, true, nil
}

func (r *LinkPreviews) Save(ctx context.Context, preview domain.Preview, now time.Time) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO link_previews (url, found, site, title, summary, photo_media_id, width, height, fetched_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (url) DO UPDATE SET found = excluded.found, site = excluded.site, title = excluded.title,
			summary = excluded.summary, photo_media_id = excluded.photo_media_id, width = excluded.width,
			height = excluded.height, fetched_at = excluded.fetched_at`,
		preview.URL, preview.Found, preview.Site, preview.Title, preview.Summary, preview.PhotoMediaID,
		preview.Width, preview.Height, now,
	)
	if err != nil {
		return fmt.Errorf("upsert link preview: %w", err)
	}

	return nil
}
