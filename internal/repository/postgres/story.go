package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "go.airlance.org/server/internal/domain/story"
	"go.airlance.org/server/internal/domain/update"
	usecase "go.airlance.org/server/internal/usecase/story"
)

const partnersOf = `
	SELECT o.subject FROM chat_members m
	JOIN chats c ON c.id = m.chat_id AND c.kind = 0
	JOIN chat_members o ON o.chat_id = m.chat_id AND o.subject <> m.subject
	WHERE m.subject = $1`

const storyColumns = `
	s.id, s.client_id, s.owner, COALESCE(p.first_name, ''), COALESCE(p.last_name, ''),
	COALESCE(p.photo_media_id, ''), COALESCE(p.video_media_id, ''),
	s.media_id, s.cover_media_id, s.is_video, s.meta, s.created_at, s.expires_at`

type Stories struct {
	pool *pgxpool.Pool
}

func NewStories(pool *pgxpool.Pool) *Stories {
	return &Stories{pool: pool}
}

func (r *Stories) Publish(ctx context.Context, next domain.Story) (domain.Story, []string, error) {
	var (
		stored   domain.Story
		audience []string
	)

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO stories (id, owner, client_id, media_id, cover_media_id, is_video, meta, created_at, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (owner, client_id) DO NOTHING`,
			next.ID, next.Owner.Subject, next.ClientID, next.MediaID, next.CoverMediaID, next.IsVideo, next.Meta,
			next.CreatedAt, next.ExpiresAt,
		)
		if err != nil {
			return fmt.Errorf("insert story: %w", err)
		}

		stored, err = readStory(ctx, tx, next.Owner.Subject, next.ClientID)
		if err != nil {
			return err
		}

		if tag.RowsAffected() == 0 {
			return nil
		}

		audience, err = announce(ctx, tx, next.Owner.Subject)

		return err
	})
	if err != nil {
		return domain.Story{}, nil, fmt.Errorf("publish story: %w", err)
	}

	return stored, audience, nil
}

func readStory(ctx context.Context, tx pgx.Tx, owner, clientID string) (domain.Story, error) {
	row := tx.QueryRow(ctx, `
		SELECT `+storyColumns+`
		FROM stories s LEFT JOIN profiles p ON p.subject = s.owner
		WHERE s.owner = $1 AND s.client_id = $2`, owner, clientID,
	)

	var next domain.Story
	if err := scanStory(row, &next); err != nil {
		return domain.Story{}, fmt.Errorf("select story: %w", err)
	}

	return next, nil
}

func scanStory(row pgx.Row, next *domain.Story, extra ...any) error {
	targets := []any{
		&next.ID, &next.ClientID, &next.Owner.Subject, &next.Owner.FirstName, &next.Owner.LastName,
		&next.Owner.PhotoMediaID, &next.Owner.VideoMediaID,
		&next.MediaID, &next.CoverMediaID, &next.IsVideo, &next.Meta, &next.CreatedAt, &next.ExpiresAt,
	}

	return row.Scan(append(targets, extra...)...)
}

func announce(ctx context.Context, tx pgx.Tx, owner string) ([]string, error) {
	rows, err := tx.Query(ctx, partnersOf, owner)
	if err != nil {
		return nil, fmt.Errorf("select story audience: %w", err)
	}

	audience, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("read story audience: %w", err)
	}

	audience = append(audience, owner)
	slices.Sort(audience)
	audience = slices.Compact(audience)

	for _, subject := range audience {
		if err := appendUpdate(ctx, tx, subject, update.KindStories); err != nil {
			return nil, err
		}
	}

	return audience, nil
}

func (r *Stories) Feed(ctx context.Context, viewer string, now time.Time, limit int) ([]domain.Story, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+storyColumns+`,
			EXISTS (SELECT 1 FROM story_views v WHERE v.story_id = s.id AND v.viewer = $1),
			CASE WHEN s.owner = $1 THEN (SELECT count(*) FROM story_views v WHERE v.story_id = s.id) ELSE 0 END,
			s.owner <> $1 AND (COALESCE(q.photos, 0) = 2 OR (COALESCE(q.photos, 0) = 1 AND t.owner IS NULL))
		FROM stories s LEFT JOIN profiles p ON p.subject = s.owner
		LEFT JOIN profile_privacy q ON q.subject = s.owner
		LEFT JOIN contacts t ON t.owner = s.owner AND t.contact = $1
		WHERE s.expires_at > $2 AND (s.owner = $1 OR s.owner IN (`+partnersOf+`))
			AND NOT EXISTS (
				SELECT 1 FROM blocks b
				WHERE (b.owner = s.owner AND b.blocked = $1) OR (b.owner = $1 AND b.blocked = s.owner)
			)
		ORDER BY s.created_at
		LIMIT $3`, viewer, now, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select stories: %w", err)
	}
	defer rows.Close()

	var stories []domain.Story

	for rows.Next() {
		var (
			next        domain.Story
			hidesPhotos bool
		)

		if err := scanStory(rows, &next, &next.Seen, &next.ViewCount, &hidesPhotos); err != nil {
			return nil, fmt.Errorf("scan story: %w", err)
		}

		if hidesPhotos {
			next.Owner.PhotoMediaID = ""
			next.Owner.VideoMediaID = ""
		}

		stories = append(stories, next)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read stories: %w", err)
	}

	return stories, nil
}

func (r *Stories) View(ctx context.Context, id, viewer string, now time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO story_views (story_id, viewer, viewed_at)
		SELECT s.id, $2::text, $3::timestamptz FROM stories s
		WHERE s.id = $1 AND s.expires_at > $3
			AND (s.owner = $2 OR s.owner IN (
				SELECT o.subject FROM chat_members m
				JOIN chats c ON c.id = m.chat_id AND c.kind = 0
				JOIN chat_members o ON o.chat_id = m.chat_id AND o.subject <> m.subject
				WHERE m.subject = $2
			))
			AND NOT EXISTS (
				SELECT 1 FROM blocks b
				WHERE (b.owner = s.owner AND b.blocked = $2) OR (b.owner = $2 AND b.blocked = s.owner)
			)
		ON CONFLICT (story_id, viewer) DO NOTHING`, id, viewer, now,
	)
	if err != nil {
		return fmt.Errorf("insert story view: %w", err)
	}

	if tag.RowsAffected() > 0 {
		return nil
	}

	var found bool

	err = r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM story_views WHERE story_id = $1 AND viewer = $2)`, id, viewer,
	).Scan(&found)
	if err != nil {
		return fmt.Errorf("select story view: %w", err)
	}

	if !found {
		return domain.ErrNotFound
	}

	return nil
}

func (r *Stories) Delete(ctx context.Context, id, owner string, now time.Time) ([]string, error) {
	var audience []string

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE stories SET expires_at = $3
			WHERE id = $1 AND owner = $2 AND expires_at > $3`, id, owner, now,
		)
		if err != nil {
			return fmt.Errorf("expire story: %w", err)
		}

		if tag.RowsAffected() == 0 {
			return domain.ErrNotFound
		}

		audience, err = announce(ctx, tx, owner)

		return err
	})
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}

		return nil, fmt.Errorf("delete story: %w", err)
	}

	return audience, nil
}

func (r *Stories) Expired(ctx context.Context, now time.Time, limit int) ([]usecase.Expired, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, media_id, cover_media_id FROM stories
		WHERE expires_at <= $1 ORDER BY expires_at LIMIT $2`, now, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select expired stories: %w", err)
	}
	defer rows.Close()

	var expired []usecase.Expired

	for rows.Next() {
		var id, photo, cover string
		if err := rows.Scan(&id, &photo, &cover); err != nil {
			return nil, fmt.Errorf("scan expired story: %w", err)
		}

		files := []string{photo}
		if cover != "" {
			files = append(files, cover)
		}

		expired = append(expired, usecase.Expired{ID: id, MediaIDs: files})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read expired stories: %w", err)
	}

	return expired, nil
}

func (r *Stories) Purge(ctx context.Context, expired []usecase.Expired) error {
	ids := make([]string, 0, len(expired))
	files := make([]string, 0, len(expired))

	for _, next := range expired {
		ids = append(ids, next.ID)
		files = append(files, next.MediaIDs...)
	}

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM story_views WHERE story_id = ANY($1)`, ids); err != nil {
			return fmt.Errorf("delete story views: %w", err)
		}

		if _, err := tx.Exec(ctx, `DELETE FROM stories WHERE id = ANY($1)`, ids); err != nil {
			return fmt.Errorf("delete stories: %w", err)
		}

		if _, err := tx.Exec(ctx, `DELETE FROM media_objects WHERE id = ANY($1)`, files); err != nil {
			return fmt.Errorf("delete story media: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("purge stories: %w", err)
	}

	return nil
}
