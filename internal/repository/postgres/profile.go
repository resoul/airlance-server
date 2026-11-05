package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

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
		SELECT first_name, last_name, username, bio, updated_at
		FROM profiles WHERE subject = $1`, subject,
	).Scan(&got.Fields.FirstName, &got.Fields.LastName, &username, &got.Fields.Bio, &got.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Profile{}, domain.ErrNotFound
	}

	if err != nil {
		return domain.Profile{}, fmt.Errorf("select profile: %w", err)
	}

	if username != nil {
		got.Fields.Username = *username
	}

	return got, nil
}

func (r *Profiles) Save(ctx context.Context, next domain.Profile) error {
	var username *string
	if next.Fields.Username != "" {
		username = &next.Fields.Username
	}

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO profiles (subject, first_name, last_name, username, bio, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (subject) DO UPDATE SET
				first_name = EXCLUDED.first_name,
				last_name = EXCLUDED.last_name,
				username = EXCLUDED.username,
				bio = EXCLUDED.bio,
				updated_at = EXCLUDED.updated_at`,
			next.Subject, next.Fields.FirstName, next.Fields.LastName, username, next.Fields.Bio, next.UpdatedAt,
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
