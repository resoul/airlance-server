package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"go.airlance.org/server/internal/domain/update"
)

type Updates struct {
	pool *pgxpool.Pool
}

func NewUpdates(pool *pgxpool.Pool) *Updates {
	return &Updates{pool: pool}
}

func (r *Updates) Latest(ctx context.Context, subject string) (uint64, error) {
	var seq int64

	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE((SELECT seq FROM update_heads WHERE subject = $1), 0)`, subject,
	).Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("select latest update: %w", err)
	}

	return uint64(seq), nil
}

func (r *Updates) After(ctx context.Context, subject string, after uint64, limit int) ([]update.Update, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT u.seq, u.kind, p.first_name, p.last_name, p.username, p.bio, p.updated_at
		FROM updates u
		JOIN profiles p ON p.subject = u.subject
		WHERE u.subject = $1 AND u.seq > $2
		ORDER BY u.seq
		LIMIT $3`, subject, int64(after), limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select updates: %w", err)
	}
	defer rows.Close()

	var updates []update.Update

	for rows.Next() {
		var (
			next     update.Update
			seq      int64
			username *string
		)

		next.Profile.Subject = subject

		fields := &next.Profile.Fields
		err := rows.Scan(
			&seq, &next.Kind, &fields.FirstName, &fields.LastName, &username, &fields.Bio, &next.Profile.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan update: %w", err)
		}

		next.Seq = uint64(seq)
		if username != nil {
			fields.Username = *username
		}

		updates = append(updates, next)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read updates: %w", err)
	}

	return updates, nil
}

func appendUpdate(ctx context.Context, tx pgx.Tx, subject string, kind update.Kind) error {
	_, err := tx.Exec(ctx, `
		WITH head AS (
			INSERT INTO update_heads (subject, seq) VALUES ($1, 1)
			ON CONFLICT (subject) DO UPDATE SET seq = update_heads.seq + 1
			RETURNING seq
		)
		INSERT INTO updates (subject, seq, kind) SELECT $1, seq, $2 FROM head`, subject, int16(kind),
	)
	if err != nil {
		return fmt.Errorf("append update: %w", err)
	}

	return nil
}
