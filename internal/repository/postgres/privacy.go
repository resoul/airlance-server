package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"go.airlance.org/server/internal/domain/directory"
	domain "go.airlance.org/server/internal/domain/privacy"
	"go.airlance.org/server/internal/domain/update"
)

type Privacy struct {
	pool *pgxpool.Pool
}

func NewPrivacy(pool *pgxpool.Pool) *Privacy {
	return &Privacy{pool: pool}
}

func (r *Privacy) Settings(ctx context.Context, subject string) (domain.Settings, error) {
	settings := domain.Defaults()

	err := r.pool.QueryRow(ctx, `
		SELECT phone, photos, bio, messages, search FROM profile_privacy WHERE subject = $1`, subject,
	).Scan(&settings.Phone, &settings.Photos, &settings.Bio, &settings.Messages, &settings.Search)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Defaults(), nil
	}

	if err != nil {
		return domain.Settings{}, fmt.Errorf("select privacy: %w", err)
	}

	return settings, nil
}

func (r *Privacy) SaveSettings(ctx context.Context, subject string, settings domain.Settings) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO profile_privacy (subject, phone, photos, bio, messages, search)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (subject) DO UPDATE SET
				phone = EXCLUDED.phone,
				photos = EXCLUDED.photos,
				bio = EXCLUDED.bio,
				messages = EXCLUDED.messages,
				search = EXCLUDED.search`,
			subject, settings.Phone, settings.Photos, settings.Bio, settings.Messages, settings.Search,
		)
		if err != nil {
			return fmt.Errorf("upsert privacy: %w", err)
		}

		return enqueueForSearch(ctx, tx, subject)
	})
	if err != nil {
		return fmt.Errorf("save privacy: %w", err)
	}

	return nil
}

func (r *Privacy) Relation(ctx context.Context, viewer, target string) (domain.Relation, error) {
	var relation domain.Relation

	err := r.pool.QueryRow(ctx, `
		SELECT
			EXISTS (SELECT 1 FROM contacts WHERE owner = $2 AND contact = $1),
			EXISTS (
				SELECT 1 FROM chat_members m
				JOIN chat_members o ON o.chat_id = m.chat_id
				WHERE m.subject = $1 AND o.subject = $2
			),
			EXISTS (SELECT 1 FROM blocks WHERE owner = $2 AND blocked = $1),
			EXISTS (SELECT 1 FROM blocks WHERE owner = $1 AND blocked = $2)`, viewer, target,
	).Scan(&relation.IsContact, &relation.SharesChat, &relation.BlockedByTarget, &relation.BlocksTarget)
	if err != nil {
		return domain.Relation{}, fmt.Errorf("select relation: %w", err)
	}

	return relation, nil
}

func (r *Privacy) SetBlocked(ctx context.Context, owner, target string, blocked bool, now time.Time) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var err error
		if blocked {
			_, err = tx.Exec(ctx, `
				INSERT INTO blocks (owner, blocked, created_at) VALUES ($1, $2, $3)
				ON CONFLICT DO NOTHING`, owner, target, now,
			)
		} else {
			_, err = tx.Exec(ctx, `DELETE FROM blocks WHERE owner = $1 AND blocked = $2`, owner, target)
		}

		if err != nil {
			return fmt.Errorf("change block: %w", err)
		}

		both := []string{owner, target}
		slices.Sort(both)

		for _, subject := range both {
			if err := appendUpdate(ctx, tx, subject, update.KindStories); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("set blocked: %w", err)
	}

	return nil
}

func (r *Privacy) Blocked(ctx context.Context, owner string) ([]directory.Person, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT b.blocked, COALESCE(p.first_name, ''), COALESCE(p.last_name, ''), COALESCE(p.username, '')
		FROM blocks b LEFT JOIN profiles p ON p.subject = b.blocked
		WHERE b.owner = $1
		ORDER BY b.created_at DESC`, owner,
	)
	if err != nil {
		return nil, fmt.Errorf("select blocked: %w", err)
	}
	defer rows.Close()

	var people []directory.Person

	for rows.Next() {
		var next directory.Person
		if err := rows.Scan(&next.Subject, &next.FirstName, &next.LastName, &next.Username); err != nil {
			return nil, fmt.Errorf("scan blocked: %w", err)
		}

		people = append(people, next)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read blocked: %w", err)
	}

	return people, nil
}

func (r *Privacy) BlockersOf(ctx context.Context, subject string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT owner FROM blocks WHERE blocked = $1`, subject)
	if err != nil {
		return nil, fmt.Errorf("select blockers: %w", err)
	}

	blockers, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("read blockers: %w", err)
	}

	return blockers, nil
}
