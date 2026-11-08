package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "go.airlance.org/server/internal/domain/media"
)

type Media struct {
	pool *pgxpool.Pool
}

func NewMedia(pool *pgxpool.Pool) *Media {
	return &Media{pool: pool}
}

func (r *Media) Create(ctx context.Context, object domain.Object) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO media_objects (id, owner, size, created_at) VALUES ($1, $2, $3, $4)`,
		object.ID, object.Owner, object.Size, object.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert media: %w", err)
	}

	return nil
}

func (r *Media) Get(ctx context.Context, id string) (domain.Object, error) {
	got := domain.Object{ID: id}

	err := r.pool.QueryRow(ctx, `
		SELECT owner, size, created_at FROM media_objects WHERE id = $1`, id,
	).Scan(&got.Owner, &got.Size, &got.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Object{}, domain.ErrNotFound
	}

	if err != nil {
		return domain.Object{}, fmt.Errorf("select media: %w", err)
	}

	return got, nil
}

func (r *Media) IsProfileMedia(ctx context.Context, id, viewer string) (bool, error) {
	var found bool

	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM profile_photos f
			LEFT JOIN profile_privacy q ON q.subject = f.subject
			WHERE (f.photo_media_id = $1 OR f.video_media_id = $1)
				AND NOT EXISTS (SELECT 1 FROM blocks b WHERE b.owner = f.subject AND b.blocked = $2)
				AND (
					COALESCE(q.photos, 0) = 0
					OR (q.photos = 1 AND EXISTS (
						SELECT 1 FROM contacts c WHERE c.owner = f.subject AND c.contact = $2
					))
				)
		)`, id, viewer,
	).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("select profile media: %w", err)
	}

	return found, nil
}

func (r *Media) IsCatalogMedia(ctx context.Context, id string) (bool, error) {
	var found bool

	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM wallpapers WHERE media_id = $1)`, id).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("select catalog media: %w", err)
	}

	return found, nil
}

func (r *Media) IsPreviewMedia(ctx context.Context, id string) (bool, error) {
	var found bool

	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM link_previews WHERE photo_media_id = $1)`, id).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("select preview media: %w", err)
	}

	return found, nil
}

func (r *Media) IsChatMedia(ctx context.Context, id, viewer string) (bool, error) {
	var found bool

	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM message_media f
			JOIN chats c ON c.id = f.chat_id
			WHERE f.media_id = $1 AND (
				c.link IS NOT NULL
				OR EXISTS (SELECT 1 FROM chat_members m WHERE m.chat_id = f.chat_id AND m.subject = $2)
			)
		)`, id, viewer,
	).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("select chat media: %w", err)
	}

	return found, nil
}

func (r *Media) IsVisibleStoryMedia(ctx context.Context, id, viewer string, now time.Time) (bool, error) {
	var found bool

	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM stories s
			WHERE (s.media_id = $1 OR s.cover_media_id = $1) AND s.expires_at > $3
				AND NOT EXISTS (
					SELECT 1 FROM blocks b
					WHERE (b.owner = s.owner AND b.blocked = $2) OR (b.owner = $2 AND b.blocked = s.owner)
				)
				AND s.owner IN (
					SELECT o.subject FROM chat_members m
					JOIN chats c ON c.id = m.chat_id AND c.kind = 0
					JOIN chat_members o ON o.chat_id = m.chat_id AND o.subject <> m.subject
					WHERE m.subject = $2
				)
		)`, id, viewer, now,
	).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("select story media: %w", err)
	}

	return found, nil
}
