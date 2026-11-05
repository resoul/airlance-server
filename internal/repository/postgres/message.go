package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"go.airlance.org/server/internal/domain/agent"
	domain "go.airlance.org/server/internal/domain/message"
)

const messageSelect = `
	SELECT m.chat_id, m.message_id, m.sender, m.client_id, m.text, m.sent_at, c.kind,
		COALESCE(p.first_name, ''), COALESCE(p.last_name, ''),
		CASE WHEN COALESCE(q.photos, 0) = 0 THEN COALESCE(p.photo_media_id, '') ELSE '' END,
		CASE WHEN COALESCE(q.photos, 0) = 0 THEN COALESCE(p.video_media_id, '') ELSE '' END,
		m.agent_id, m.agent_name, m.attachments, m.forward_name
	FROM messages m
	JOIN chats c ON c.id = m.chat_id
	LEFT JOIN profiles p ON p.subject = m.sender
	LEFT JOIN profile_privacy q ON q.subject = m.sender`

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

		if err := checkMayWrite(ctx, tx, next.ChatID, next.Sender, members); err != nil {
			return err
		}

		if err := checkOwnsMedia(ctx, tx, next.Sender, next.MediaIDs()); err != nil {
			return err
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

		if err := linkMedia(ctx, tx, next.ChatID, next.MediaIDs()); err != nil {
			return err
		}

		if next.AgentID != "" {
			return nil
		}

		return enqueueChatEvent(ctx, tx, agent.TriggerKeyword, next.ChatID, next.Sender, next.Text, next.SentAt)
	})
	if err != nil {
		return domain.Message{}, nil, false, fmt.Errorf("send: %w", err)
	}

	return stored, members, created, nil
}

func (r *Messages) CheckWrite(ctx context.Context, chatID, sender string) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		members, err := membersOf(ctx, tx, chatID)
		if err != nil {
			return err
		}

		if !slices.Contains(members, sender) {
			return domain.ErrChatNotFound
		}

		return checkMayWrite(ctx, tx, chatID, sender, members)
	})
	if err != nil {
		return fmt.Errorf("check write: %w", err)
	}

	return nil
}

func (r *Messages) RoleOf(ctx context.Context, chatID, subject string) (domain.Role, bool, error) {
	var (
		role  domain.Role
		found bool
	)

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var err error

		role, found, err = roleIn(ctx, tx, chatID, subject)

		return err
	})
	if err != nil {
		return 0, false, fmt.Errorf("read role: %w", err)
	}

	return role, found, nil
}

func checkMayWrite(ctx context.Context, tx pgx.Tx, chatID, sender string, members []string) error {
	var (
		kind domain.Kind
		role domain.Role
	)

	err := tx.QueryRow(ctx, `
		SELECT c.kind, m.role FROM chats c
		JOIN chat_members m ON m.chat_id = c.id AND m.subject = $2
		WHERE c.id = $1`, chatID, sender,
	).Scan(&kind, &role)
	if err != nil {
		return fmt.Errorf("select chat rules: %w", err)
	}

	if kind == domain.KindChannel && role < domain.RoleAdmin {
		return domain.ErrNotAllowed
	}

	if kind != domain.KindDirect {
		return nil
	}

	var isBlocked bool

	err = tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM blocks
			WHERE (owner = $1 AND blocked = ANY($2)) OR (owner = ANY($2) AND blocked = $1)
		)`, sender, members,
	).Scan(&isBlocked)
	if err != nil {
		return fmt.Errorf("check blocks: %w", err)
	}

	if isBlocked {
		return domain.ErrBlocked
	}

	return nil
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
	attachments, err := encodeAttachments(next.Attachments)
	if err != nil {
		return domain.Message{}, false, err
	}

	tag, err := tx.Exec(ctx, `
		INSERT INTO messages (
			chat_id, message_id, sender, client_id, text, sent_at, agent_id, agent_name, attachments, forward_name
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (chat_id, sender, client_id) DO NOTHING`,
		next.ChatID, next.ID, next.Sender, next.ClientID, next.Text, next.SentAt, next.AgentID, next.AgentName,
		attachments, next.ForwardName,
	)
	if err != nil {
		return domain.Message{}, false, fmt.Errorf("insert message: %w", err)
	}

	if tag.RowsAffected() == 1 {
		return next, true, nil
	}

	stored := domain.Message{ChatID: next.ChatID, Sender: next.Sender, ClientID: next.ClientID}

	var saved []byte

	err = tx.QueryRow(ctx, `
		SELECT message_id, text, sent_at, attachments, forward_name
		FROM messages WHERE chat_id = $1 AND sender = $2 AND client_id = $3`,
		next.ChatID, next.Sender, next.ClientID,
	).Scan(&stored.ID, &stored.Text, &stored.SentAt, &saved, &stored.ForwardName)
	if err != nil {
		return domain.Message{}, false, fmt.Errorf("select repeated message: %w", err)
	}

	stored.Attachments, err = decodeAttachments(saved)
	if err != nil {
		return domain.Message{}, false, err
	}

	return stored, false, nil
}

func (r *Messages) History(
	ctx context.Context,
	chatID, subject string,
	before int64,
	limit int,
) ([]domain.Message, error) {
	var mayRead bool

	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM chat_members WHERE chat_id = $1 AND subject = $2)
			OR EXISTS (SELECT 1 FROM chats WHERE id = $1 AND link IS NOT NULL)`, chatID, subject,
	).Scan(&mayRead)
	if err != nil {
		return nil, fmt.Errorf("check member: %w", err)
	}

	if !mayRead {
		return nil, domain.ErrChatNotFound
	}

	rows, err := r.pool.Query(ctx, messageSelect+`
		WHERE m.chat_id = $1 AND ($2 = 0 OR m.message_id < $2)
		ORDER BY m.message_id DESC
		LIMIT $3`, chatID, before, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select history: %w", err)
	}

	messages, err := collectMessages(rows)
	if err != nil {
		return nil, err
	}

	for index := range messages {
		messages[index] = messages[index].ForViewer(subject)
	}

	return messages, nil
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

	rows, err := pool.Query(ctx, messageSelect+`
		WHERE (m.chat_id, m.message_id) IN (SELECT * FROM unnest($1::text[], $2::bigint[]))`, chatIDs, ids,
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

		var attachments []byte

		author := &next.Author
		err := row.Scan(
			&next.ChatID, &next.ID, &next.Sender, &next.ClientID, &next.Text, &next.SentAt, &next.ChatKind,
			&author.FirstName, &author.LastName, &author.PhotoMediaID, &author.VideoMediaID,
			&next.AgentID, &next.AgentName, &attachments, &next.ForwardName,
		)
		if err != nil {
			return next, err
		}

		next.Attachments, err = decodeAttachments(attachments)

		return next, err
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("read messages: %w", err)
	}

	return messages, nil
}
