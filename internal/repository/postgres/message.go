package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "go.airlance.org/server/internal/domain/message"
)

type Messages struct {
	pool *pgxpool.Pool
}

func NewMessages(pool *pgxpool.Pool) *Messages {
	return &Messages{pool: pool}
}

func (r *Messages) OpenDirect(ctx context.Context, candidateID, first, second string, now time.Time) (string, error) {
	id := candidateID

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			WITH created AS (
				INSERT INTO chats (id, direct_key, created_at) VALUES ($1, $2, $3)
				ON CONFLICT (direct_key) DO NOTHING
				RETURNING id
			)
			SELECT id FROM created
			UNION ALL
			SELECT id FROM chats WHERE direct_key = $2
			LIMIT 1`, candidateID, domain.DirectKey(first, second), now,
		).Scan(&id)
		if err != nil {
			return fmt.Errorf("insert chat: %w", err)
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO chat_members (chat_id, subject) VALUES ($1, $2), ($1, $3)
			ON CONFLICT DO NOTHING`, id, first, second,
		)
		if err != nil {
			return fmt.Errorf("insert members: %w", err)
		}

		return nil
	})
	if err != nil {
		return "", fmt.Errorf("open direct chat: %w", err)
	}

	return id, nil
}

func (r *Messages) Send(ctx context.Context, next domain.Message) (domain.Message, []string, bool, error) {
	stored := next

	var (
		members []string
		created bool
	)

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var err error

		members, err = membersOf(ctx, tx, next.ChatID)
		if err != nil {
			return err
		}

		if !slices.Contains(members, next.Sender) {
			return domain.ErrChatNotFound
		}

		var isBlocked bool

		err = tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM blocks
				WHERE (owner = $1 AND blocked = ANY($2)) OR (owner = ANY($2) AND blocked = $1)
			)`, next.Sender, members,
		).Scan(&isBlocked)
		if err != nil {
			return fmt.Errorf("check blocks: %w", err)
		}

		if isBlocked {
			return domain.ErrBlocked
		}

		stored, created, err = insertMessage(ctx, tx, next)
		if err != nil || !created {
			return err
		}

		_, err = tx.Exec(ctx, `
			UPDATE chats SET last_message_id = GREATEST(COALESCE(last_message_id, 0), $2) WHERE id = $1`,
			next.ChatID, next.ID,
		)
		if err != nil {
			return fmt.Errorf("update chat: %w", err)
		}

		for _, member := range members {
			if err := appendMessageUpdate(ctx, tx, member, next.ChatID, next.ID); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return domain.Message{}, nil, false, fmt.Errorf("send: %w", err)
	}

	return stored, members, created, nil
}

func membersOf(ctx context.Context, tx pgx.Tx, chatID string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT subject FROM chat_members WHERE chat_id = $1 ORDER BY subject`, chatID)
	if err != nil {
		return nil, fmt.Errorf("select members: %w", err)
	}

	members, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("read members: %w", err)
	}

	return members, nil
}

func insertMessage(ctx context.Context, tx pgx.Tx, next domain.Message) (domain.Message, bool, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO messages (chat_id, message_id, sender, client_id, text, sent_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (chat_id, sender, client_id) DO NOTHING`,
		next.ChatID, next.ID, next.Sender, next.ClientID, next.Text, next.SentAt,
	)
	if err != nil {
		return domain.Message{}, false, fmt.Errorf("insert message: %w", err)
	}

	if tag.RowsAffected() == 1 {
		return next, true, nil
	}

	stored := domain.Message{ChatID: next.ChatID, Sender: next.Sender, ClientID: next.ClientID}

	err = tx.QueryRow(ctx, `
		SELECT message_id, text, sent_at FROM messages WHERE chat_id = $1 AND sender = $2 AND client_id = $3`,
		next.ChatID, next.Sender, next.ClientID,
	).Scan(&stored.ID, &stored.Text, &stored.SentAt)
	if err != nil {
		return domain.Message{}, false, fmt.Errorf("select repeated message: %w", err)
	}

	return stored, false, nil
}

func (r *Messages) History(
	ctx context.Context,
	chatID, subject string,
	before int64,
	limit int,
) ([]domain.Message, error) {
	var isMember bool

	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM chat_members WHERE chat_id = $1 AND subject = $2)`, chatID, subject,
	).Scan(&isMember)
	if err != nil {
		return nil, fmt.Errorf("check member: %w", err)
	}

	if !isMember {
		return nil, domain.ErrChatNotFound
	}

	rows, err := r.pool.Query(ctx, `
		SELECT chat_id, message_id, sender, client_id, text, sent_at
		FROM messages
		WHERE chat_id = $1 AND ($2 = 0 OR message_id < $2)
		ORDER BY message_id DESC
		LIMIT $3`, chatID, before, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select history: %w", err)
	}

	return collectMessages(rows)
}

func (r *Messages) Chats(ctx context.Context, subject string, before int64, limit int) ([]domain.Chat, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.id, c.last_message_id, o.subject, COALESCE(p.first_name, ''), COALESCE(p.last_name, ''),
			COALESCE(p.username, ''),
			CASE WHEN COALESCE(q.photos, 0) = 2 OR b.owner IS NOT NULL OR (q.photos = 1 AND t.owner IS NULL) THEN ''
				ELSE COALESCE(p.photo_media_id, '') END,
			CASE WHEN COALESCE(q.photos, 0) = 2 OR b.owner IS NOT NULL OR (q.photos = 1 AND t.owner IS NULL) THEN ''
				ELSE COALESCE(p.video_media_id, '') END
		FROM chat_members m
		JOIN chats c ON c.id = m.chat_id
		JOIN chat_members o ON o.chat_id = c.id AND o.subject <> m.subject
		LEFT JOIN profiles p ON p.subject = o.subject
		LEFT JOIN profile_privacy q ON q.subject = o.subject
		LEFT JOIN blocks b ON b.owner = o.subject AND b.blocked = m.subject
		LEFT JOIN contacts t ON t.owner = o.subject AND t.contact = m.subject
		WHERE m.subject = $1 AND c.last_message_id IS NOT NULL AND ($2 = 0 OR c.last_message_id < $2)
		ORDER BY c.last_message_id DESC
		LIMIT $3`, subject, before, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select chats: %w", err)
	}
	defer rows.Close()

	var (
		chats []domain.Chat
		refs  []messageRef
	)

	for rows.Next() {
		var (
			chat domain.Chat
			last int64
		)

		peer := &chat.Peer
		err := rows.Scan(
			&chat.ID, &last, &peer.Subject, &peer.FirstName, &peer.LastName, &peer.Username,
			&peer.PhotoMediaID, &peer.VideoMediaID,
		)
		if err != nil {
			return nil, fmt.Errorf("scan chat: %w", err)
		}

		chats = append(chats, chat)
		refs = append(refs, messageRef{chatID: chat.ID, id: last})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read chats: %w", err)
	}

	found, err := messagesByRef(ctx, r.pool, refs)
	if err != nil {
		return nil, err
	}

	for index, ref := range refs {
		if last, ok := found[ref]; ok {
			chats[index].Last = &last
		}
	}

	return chats, nil
}

type messageRef struct {
	chatID string
	id     int64
}

func messagesByRef(ctx context.Context, pool *pgxpool.Pool, refs []messageRef) (map[messageRef]domain.Message, error) {
	found := make(map[messageRef]domain.Message, len(refs))
	if len(refs) == 0 {
		return found, nil
	}

	chatIDs := make([]string, len(refs))
	ids := make([]int64, len(refs))

	for index, ref := range refs {
		chatIDs[index] = ref.chatID
		ids[index] = ref.id
	}

	rows, err := pool.Query(ctx, `
		SELECT chat_id, message_id, sender, client_id, text, sent_at
		FROM messages
		WHERE (chat_id, message_id) IN (SELECT * FROM unnest($1::text[], $2::bigint[]))`, chatIDs, ids,
	)
	if err != nil {
		return nil, fmt.Errorf("select messages: %w", err)
	}

	messages, err := collectMessages(rows)
	if err != nil {
		return nil, err
	}

	for _, next := range messages {
		found[messageRef{chatID: next.ChatID, id: next.ID}] = next
	}

	return found, nil
}

func collectMessages(rows pgx.Rows) ([]domain.Message, error) {
	messages, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Message, error) {
		var next domain.Message

		err := row.Scan(&next.ChatID, &next.ID, &next.Sender, &next.ClientID, &next.Text, &next.SentAt)

		return next, err
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("read messages: %w", err)
	}

	return messages, nil
}
