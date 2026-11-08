package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/update"
)

func (r *Messages) SetRead(
	ctx context.Context,
	chatID, subject string,
	upTo int64,
) (domain.ReadState, []string, error) {
	var woken []string

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		members, err := membersOf(ctx, tx, chatID)
		if err != nil {
			return err
		}

		if !slices.Contains(members, subject) {
			return domain.ErrChatNotFound
		}

		moved, err := moveRead(ctx, tx, chatID, subject, upTo)
		if err != nil || !moved {
			return err
		}

		woken, err = readAudience(ctx, tx, chatID, subject, members)
		if err != nil {
			return err
		}

		return announceRead(ctx, tx, woken, chatID)
	})
	if err != nil {
		return domain.ReadState{}, nil, fmt.Errorf("set read: %w", err)
	}

	state, err := readState(ctx, r.pool, subject, chatID)
	if err != nil {
		return domain.ReadState{}, nil, err
	}

	return state, woken, nil
}

func moveRead(ctx context.Context, tx pgx.Tx, chatID, subject string, upTo int64) (bool, error) {
	if upTo < 0 {
		return false, domain.ErrInvalidRead
	}

	tag, err := tx.Exec(ctx, `
		UPDATE chat_members SET delivered_up_to = GREATEST(delivered_up_to, LEAST($3, COALESCE((
			SELECT max(message_id) FROM messages WHERE chat_id = $1 AND thread_id = 0
		), 0))), read_up_to = LEAST($3, COALESCE((
			SELECT max(message_id) FROM messages WHERE chat_id = $1 AND thread_id = 0
		), 0))
		WHERE chat_id = $1 AND subject = $2 AND read_up_to < LEAST($3, COALESCE((
			SELECT max(message_id) FROM messages WHERE chat_id = $1 AND thread_id = 0
		), 0))`, chatID, subject, upTo,
	)
	if err != nil {
		return false, fmt.Errorf("move read pointer: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

func readAudience(ctx context.Context, tx pgx.Tx, chatID, subject string, members []string) ([]string, error) {
	var kind domain.Kind

	if err := tx.QueryRow(ctx, `SELECT kind FROM chats WHERE id = $1`, chatID).Scan(&kind); err != nil {
		return nil, fmt.Errorf("select chat kind: %w", err)
	}

	if kind != domain.KindDirect {
		return []string{subject}, nil
	}

	return members, nil
}

func announceRead(ctx context.Context, tx pgx.Tx, members []string, chatID string) error {
	for _, member := range members {
		_, err := tx.Exec(ctx, `
			WITH head AS (
				INSERT INTO update_heads (subject, seq) VALUES ($1, 1)
				ON CONFLICT (subject) DO UPDATE SET seq = update_heads.seq + 1
				RETURNING seq
			)
			INSERT INTO updates (subject, seq, kind, chat_id) SELECT $1, seq, $2, $3 FROM head`,
			member, int16(update.KindRead), chatID,
		)
		if err != nil {
			return fmt.Errorf("append read update: %w", err)
		}
	}

	return nil
}

func readState(ctx context.Context, pool *pgxpool.Pool, subject, chatID string) (domain.ReadState, error) {
	state := domain.ReadState{ChatID: chatID}

	err := pool.QueryRow(ctx, `
		SELECT m.read_up_to, COALESCE(o.read_up_to, 0), COALESCE(o.delivered_up_to, 0), (
			SELECT count(*) FROM (
				SELECT 1 FROM messages x
				WHERE x.chat_id = m.chat_id AND x.thread_id = 0 AND x.deleted_at IS NULL AND x.sender <> $2
					AND x.message_id > m.read_up_to AND NOT EXISTS (
						SELECT 1 FROM message_hidden h
						WHERE h.subject = $2 AND h.chat_id = x.chat_id AND h.message_id = x.message_id
					)
				LIMIT 1000
			) u
		)
		FROM chat_members m
		JOIN chats c ON c.id = m.chat_id
		LEFT JOIN chat_members o ON c.kind = 0 AND o.chat_id = m.chat_id AND o.subject <> $2
		WHERE m.chat_id = $1 AND m.subject = $2`, chatID, subject,
	).Scan(&state.MyReadID, &state.PeerReadID, &state.PeerDeliveredID, &state.Unread)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, nil
	}

	if err != nil {
		return domain.ReadState{}, fmt.Errorf("select read state: %w", err)
	}

	return state, nil
}

func (r *Messages) SetDelivered(
	ctx context.Context,
	chatID, subject string,
	upTo int64,
) (domain.ReadState, []string, error) {
	var woken []string

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		members, err := membersOf(ctx, tx, chatID)
		if err != nil {
			return err
		}

		if !slices.Contains(members, subject) {
			return domain.ErrChatNotFound
		}

		if upTo < 0 {
			return domain.ErrInvalidRead
		}

		var kind domain.Kind

		if err := tx.QueryRow(ctx, `SELECT kind FROM chats WHERE id = $1`, chatID).Scan(&kind); err != nil {
			return fmt.Errorf("select chat kind: %w", err)
		}

		if kind != domain.KindDirect {
			return nil
		}

		tag, err := tx.Exec(ctx, `
			UPDATE chat_members SET delivered_up_to = LEAST($3, COALESCE((
				SELECT max(message_id) FROM messages WHERE chat_id = $1 AND thread_id = 0
			), 0))
			WHERE chat_id = $1 AND subject = $2 AND delivered_up_to < LEAST($3, COALESCE((
				SELECT max(message_id) FROM messages WHERE chat_id = $1 AND thread_id = 0
			), 0))`, chatID, subject, upTo,
		)
		if err != nil {
			return fmt.Errorf("move delivered pointer: %w", err)
		}

		if tag.RowsAffected() == 0 {
			return nil
		}

		woken = members

		return announceRead(ctx, tx, woken, chatID)
	})
	if err != nil {
		return domain.ReadState{}, nil, fmt.Errorf("set delivered: %w", err)
	}

	state, err := readState(ctx, r.pool, subject, chatID)
	if err != nil {
		return domain.ReadState{}, nil, err
	}

	return state, woken, nil
}
