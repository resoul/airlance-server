package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "go.airlance.org/server/internal/domain/contact"
	"go.airlance.org/server/internal/domain/update"
	usecase "go.airlance.org/server/internal/usecase/contact"
)

type Contacts struct {
	pool *pgxpool.Pool
}

func NewContacts(pool *pgxpool.Pool) *Contacts {
	return &Contacts{pool: pool}
}

func (r *Contacts) Add(ctx context.Context, next usecase.Added) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO contacts (owner, contact, custom_first_name, custom_last_name, from_device, added_at)
			VALUES ($1, $2, $3, $4, false, $5)
			ON CONFLICT (owner, contact) DO UPDATE SET
				custom_first_name = EXCLUDED.custom_first_name,
				custom_last_name = EXCLUDED.custom_last_name,
				from_device = false`,
			next.Owner, next.Subject, next.CustomFirstName, next.CustomLastName, next.AddedAt,
		)
		if err != nil {
			return fmt.Errorf("insert contact: %w", err)
		}

		return appendUpdate(ctx, tx, next.Owner, update.KindContacts)
	})
	if err != nil {
		return fmt.Errorf("add contact: %w", err)
	}

	return nil
}

func (r *Contacts) Remove(ctx context.Context, owner, subject string) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM contacts WHERE owner = $1 AND contact = $2`, owner, subject)
		if err != nil {
			return fmt.Errorf("delete contact: %w", err)
		}

		return appendUpdate(ctx, tx, owner, update.KindContacts)
	})
	if err != nil {
		return fmt.Errorf("remove contact: %w", err)
	}

	return nil
}

func (r *Contacts) List(ctx context.Context, owner string) ([]domain.Contact, error) {
	rows, err := r.pool.Query(ctx, `
		WITH seen AS (
			SELECT c.contact, c.custom_first_name, c.custom_last_name, c.from_device,
				COALESCE(q.phone, 2) AS phone_level, COALESCE(q.photos, 0) AS photo_level,
				EXISTS (SELECT 1 FROM contacts t WHERE t.owner = c.contact AND t.contact = c.owner) AS is_mutual
			FROM contacts c
			LEFT JOIN profile_privacy q ON q.subject = c.contact
			WHERE c.owner = $1
				AND NOT EXISTS (SELECT 1 FROM blocks b WHERE b.owner = c.contact AND b.blocked = c.owner)
		)
		SELECT s.contact, COALESCE(p.first_name, ''), COALESCE(p.last_name, ''), COALESCE(p.username, ''),
			CASE WHEN s.photo_level = 0 OR (s.photo_level = 1 AND s.is_mutual) THEN COALESCE(p.photo_media_id, '') ELSE '' END,
			CASE WHEN s.photo_level = 0 OR (s.photo_level = 1 AND s.is_mutual) THEN COALESCE(p.video_media_id, '') ELSE '' END,
			s.custom_first_name, s.custom_last_name,
			CASE WHEN s.phone_level = 0 OR (s.phone_level = 1 AND s.is_mutual) THEN COALESCE(p.phone, '') ELSE '' END,
			s.from_device
		FROM seen s
		LEFT JOIN profiles p ON p.subject = s.contact
		ORDER BY lower(COALESCE(NULLIF(s.custom_first_name, ''), p.first_name, ''))`, owner,
	)
	if err != nil {
		return nil, fmt.Errorf("select contacts: %w", err)
	}
	defer rows.Close()

	var contacts []domain.Contact

	for rows.Next() {
		var next domain.Contact

		err := rows.Scan(
			&next.Subject, &next.FirstName, &next.LastName, &next.Username,
			&next.PhotoMediaID, &next.VideoMediaID,
			&next.CustomFirstName, &next.CustomLastName, &next.Phone, &next.FromDevice,
		)
		if err != nil {
			return nil, fmt.Errorf("scan contact: %w", err)
		}

		contacts = append(contacts, next)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read contacts: %w", err)
	}

	return contacts, nil
}

func (r *Contacts) FindByPhone(ctx context.Context, owner, digits string) ([]string, error) {
	found, err := r.match(ctx, r.pool, owner, []string{digits})
	if err != nil {
		return nil, err
	}

	subjects := make([]string, 0, len(found))
	for subject := range found {
		subjects = append(subjects, subject)
	}

	return subjects, nil
}

func (r *Contacts) Sync(ctx context.Context, owner string, phones []string, now time.Time) (map[string]string, error) {
	var matched map[string]string

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var err error

		matched, err = r.match(ctx, tx, owner, phones)
		if err != nil {
			return err
		}

		subjects := make([]string, 0, len(matched))
		for subject := range matched {
			subjects = append(subjects, subject)
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO contacts (owner, contact, from_device, added_at)
			SELECT $1, found, true, $3 FROM unnest($2::text[]) AS found
			ON CONFLICT (owner, contact) DO NOTHING`, owner, subjects, now,
		)
		if err != nil {
			return fmt.Errorf("insert matches: %w", err)
		}

		_, err = tx.Exec(ctx, `
			DELETE FROM contacts WHERE owner = $1 AND from_device AND NOT (contact = ANY($2::text[]))`, owner, subjects,
		)
		if err != nil {
			return fmt.Errorf("delete old matches: %w", err)
		}

		return appendUpdate(ctx, tx, owner, update.KindContacts)
	})
	if err != nil {
		return nil, fmt.Errorf("sync contacts: %w", err)
	}

	return matched, nil
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (r *Contacts) match(ctx context.Context, db querier, owner string, phones []string) (map[string]string, error) {
	rows, err := db.Query(ctx, `
		SELECT p.subject, p.phone_digits
		FROM profiles p
		LEFT JOIN profile_privacy q ON q.subject = p.subject
		WHERE p.phone_digits = ANY($2::text[]) AND p.subject <> $1
			AND COALESCE(q.search, 0) <> 2
			AND NOT EXISTS (SELECT 1 FROM blocks b WHERE b.owner = p.subject AND b.blocked = $1)`, owner, phones,
	)
	if err != nil {
		return nil, fmt.Errorf("select matches: %w", err)
	}
	defer rows.Close()

	matched := map[string]string{}

	for rows.Next() {
		var subject, digits string
		if err := rows.Scan(&subject, &digits); err != nil {
			return nil, fmt.Errorf("scan match: %w", err)
		}

		matched[subject] = digits
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read matches: %w", err)
	}

	return matched, nil
}
