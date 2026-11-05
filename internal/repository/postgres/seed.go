package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"go.airlance.org/server/internal/domain/update"
	"go.airlance.org/server/internal/usecase/seed"
)

type Seeds struct {
	pool *pgxpool.Pool
}

func NewSeeds(pool *pgxpool.Pool) *Seeds {
	return &Seeds{pool: pool}
}

func (r *Seeds) SubjectOfUsername(ctx context.Context, username string) (string, error) {
	var subject string

	err := r.pool.QueryRow(ctx, `SELECT subject FROM profiles WHERE lower(username) = lower($1)`, username).Scan(&subject)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", seed.ErrPersonNotFound
	}

	if err != nil {
		return "", fmt.Errorf("select username: %w", err)
	}

	return subject, nil
}

func (r *Seeds) Purge(ctx context.Context, prefix string, _ time.Time) (seed.Purged, error) {
	var purged seed.Purged

	pattern := prefix + "%"

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var err error

		purged.Subjects, err = texts(ctx, tx, `SELECT subject FROM profiles WHERE subject LIKE $1`, pattern)
		if err != nil {
			return err
		}

		purged.MediaIDs, err = texts(ctx, tx, `SELECT id FROM media_objects WHERE owner LIKE $1`, pattern)
		if err != nil {
			return err
		}

		chats, err := texts(ctx, tx, `
			SELECT c.id FROM chats c
			WHERE c.created_by LIKE $1 OR (c.kind = 0 AND EXISTS (
				SELECT 1 FROM chat_members m WHERE m.chat_id = c.id AND m.subject LIKE $1
			))`, pattern)
		if err != nil {
			return err
		}

		purged.Woken, err = texts(ctx, tx, `
			SELECT DISTINCT subject FROM (
				SELECT subject FROM chat_members WHERE chat_id = ANY($2) AND subject NOT LIKE $1
				UNION
				SELECT owner FROM contacts WHERE contact LIKE $1 AND owner NOT LIKE $1
			) people`, pattern, chats)
		if err != nil {
			return err
		}

		if err := purgeRows(ctx, tx, pattern, chats); err != nil {
			return err
		}

		for _, subject := range purged.Woken {
			if err := appendUpdate(ctx, tx, subject, update.KindChats); err != nil {
				return err
			}

			if err := appendUpdate(ctx, tx, subject, update.KindContacts); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return seed.Purged{}, fmt.Errorf("purge: %w", err)
	}

	return purged, nil
}

func texts(ctx context.Context, tx pgx.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select: %w", err)
	}

	found, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	return found, nil
}

func purgeRows(ctx context.Context, tx pgx.Tx, pattern string, chats []string) error {
	statements := []struct {
		query string
		args  []any
	}{
		{`UPDATE updates SET kind = $2, chat_id = NULL, message_id = NULL WHERE chat_id = ANY($1)`,
			[]any{chats, int16(update.KindChats)}},
		{`DELETE FROM messages WHERE chat_id = ANY($1)`, []any{chats}},
		{`DELETE FROM chat_members WHERE chat_id = ANY($1)`, []any{chats}},
		{`DELETE FROM chats WHERE id = ANY($1)`, []any{chats}},
		{`DELETE FROM agents WHERE owner LIKE $1`, []any{pattern}},
		{`DELETE FROM scheduled_messages WHERE sender LIKE $1`, []any{pattern}},
		{`DELETE FROM contacts WHERE owner LIKE $1 OR contact LIKE $1`, []any{pattern}},
		{`DELETE FROM blocks WHERE owner LIKE $1 OR blocked LIKE $1`, []any{pattern}},
		{`DELETE FROM profile_privacy WHERE subject LIKE $1`, []any{pattern}},
		{`DELETE FROM profile_photos WHERE subject LIKE $1`, []any{pattern}},
		{`DELETE FROM search_queue WHERE subject LIKE $1`, []any{pattern}},
		{`DELETE FROM profiles WHERE subject LIKE $1`, []any{pattern}},
		{`DELETE FROM media_objects WHERE owner LIKE $1`, []any{pattern}},
		{`DELETE FROM updates WHERE subject LIKE $1`, []any{pattern}},
		{`DELETE FROM update_heads WHERE subject LIKE $1`, []any{pattern}},
	}

	for _, next := range statements {
		if _, err := tx.Exec(ctx, next.query, next.args...); err != nil {
			return fmt.Errorf("purge %q: %w", next.query, err)
		}
	}

	return nil
}
