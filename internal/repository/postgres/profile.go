package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"go.airlance.org/server/internal/domain/contact"
	domain "go.airlance.org/server/internal/domain/profile"
	"go.airlance.org/server/internal/domain/update"
)

const uniqueViolation = "23505"

type Profiles struct {
	pool *pgxpool.Pool
}

func NewProfiles(pool *pgxpool.Pool) *Profiles {
	return &Profiles{pool: pool}
}

func (r *Profiles) Get(ctx context.Context, subject string) (domain.Profile, error) {
	var (
		got      = domain.Profile{Subject: subject}
		username *string
	)

	err := r.pool.QueryRow(ctx, `
		SELECT first_name, last_name, username, bio, phone, photo_media_id, video_media_id, updated_at
		FROM profiles WHERE subject = $1`, subject,
	).Scan(
		&got.Fields.FirstName, &got.Fields.LastName, &username, &got.Fields.Bio,
		&got.Fields.Phone, &got.Fields.PhotoMediaID, &got.Fields.VideoMediaID, &got.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Profile{}, domain.ErrNotFound
	}

	if err != nil {
		return domain.Profile{}, fmt.Errorf("select profile: %w", err)
	}

	if username != nil {
		got.Fields.Username = *username
	}

	got.Photos, err = r.photos(ctx, subject)
	if err != nil {
		return domain.Profile{}, err
	}

	return got, nil
}

func (r *Profiles) photos(ctx context.Context, subject string) ([]domain.Photo, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT photo_media_id, video_media_id, added_at
		FROM profile_photos WHERE subject = $1 ORDER BY position DESC`, subject,
	)
	if err != nil {
		return nil, fmt.Errorf("select photos: %w", err)
	}
	defer rows.Close()

	var photos []domain.Photo

	for rows.Next() {
		var next domain.Photo
		if err := rows.Scan(&next.PhotoMediaID, &next.VideoMediaID, &next.AddedAt); err != nil {
			return nil, fmt.Errorf("scan photo: %w", err)
		}

		photos = append(photos, next)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read photos: %w", err)
	}

	return photos, nil
}

func (r *Profiles) AddPhoto(ctx context.Context, subject string, photo domain.Photo) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if err := lockProfile(ctx, tx, subject); err != nil {
			return err
		}

		_, err := tx.Exec(ctx, `
			INSERT INTO profile_photos (subject, photo_media_id, video_media_id, added_at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (subject, photo_media_id) DO UPDATE SET position = nextval('profile_photos_position')`,
			subject, photo.PhotoMediaID, photo.VideoMediaID, photo.AddedAt,
		)
		if err != nil {
			return fmt.Errorf("insert photo: %w", err)
		}

		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM profile_photos WHERE subject = $1`, subject).Scan(&count); err != nil {
			return fmt.Errorf("count photos: %w", err)
		}

		if count > domain.MaxPhotos {
			return domain.ErrTooManyPhotos
		}

		return refreshAvatar(ctx, tx, subject, photo.AddedAt)
	})
}

func (r *Profiles) RemovePhoto(ctx context.Context, subject, photoMediaID string, now time.Time) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if err := lockProfile(ctx, tx, subject); err != nil {
			return err
		}

		_, err := tx.Exec(ctx, `
			DELETE FROM profile_photos WHERE subject = $1 AND photo_media_id = $2`, subject, photoMediaID,
		)
		if err != nil {
			return fmt.Errorf("delete photo: %w", err)
		}

		return refreshAvatar(ctx, tx, subject, now.UTC())
	})
}

func lockProfile(ctx context.Context, tx pgx.Tx, subject string) error {
	var found int

	err := tx.QueryRow(ctx, `SELECT 1 FROM profiles WHERE subject = $1 FOR UPDATE`, subject).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}

	if err != nil {
		return fmt.Errorf("lock profile: %w", err)
	}

	return nil
}

func refreshAvatar(ctx context.Context, tx pgx.Tx, subject string, now time.Time) error {
	_, err := tx.Exec(ctx, `
		UPDATE profiles p SET
			photo_media_id = COALESCE(newest.photo_media_id, ''),
			video_media_id = COALESCE(newest.video_media_id, ''),
			updated_at = $2
		FROM profiles base
		LEFT JOIN LATERAL (
			SELECT photo_media_id, video_media_id FROM profile_photos
			WHERE subject = base.subject ORDER BY position DESC LIMIT 1
		) newest ON true
		WHERE base.subject = $1 AND p.subject = base.subject`, subject, now,
	)
	if err != nil {
		return fmt.Errorf("refresh avatar: %w", err)
	}

	if err := appendUpdate(ctx, tx, subject, update.KindProfile); err != nil {
		return err
	}

	return enqueueForSearch(ctx, tx, subject)
}

func (r *Profiles) Save(ctx context.Context, next domain.Profile) error {
	var username *string
	if next.Fields.Username != "" {
		username = &next.Fields.Username
	}

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO profiles (subject, first_name, last_name, username, bio, phone, phone_digits, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (subject) DO UPDATE SET
				first_name = EXCLUDED.first_name,
				last_name = EXCLUDED.last_name,
				username = EXCLUDED.username,
				bio = EXCLUDED.bio,
				phone = EXCLUDED.phone,
				phone_digits = EXCLUDED.phone_digits,
				updated_at = EXCLUDED.updated_at`,
			next.Subject, next.Fields.FirstName, next.Fields.LastName, username, next.Fields.Bio,
			next.Fields.Phone, contact.Digits(next.Fields.Phone), next.UpdatedAt,
		)
		if err != nil {
			return err
		}

		if err := appendUpdate(ctx, tx, next.Subject, update.KindProfile); err != nil {
			return err
		}

		return enqueueForSearch(ctx, tx, next.Subject)
	})

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return domain.ErrUsernameTaken
	}

	if err != nil {
		return fmt.Errorf("save profile: %w", err)
	}

	return nil
}
