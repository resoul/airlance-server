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
	"go.airlance.org/server/internal/domain/update"
)

func (r *Messages) Edit(
	ctx context.Context,
	chatID string,
	id int64,
	subject, text string,
	entities []domain.Entity,
	now time.Time,
) (domain.Message, []string, error) {
	var members []string

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var err error

		members, err = membersOf(ctx, tx, chatID)
		if err != nil {
			return err
		}

		if !slices.Contains(members, subject) {
			return domain.ErrChatNotFound
		}

		current, attachments, err := lockMessage(ctx, tx, chatID, id)
		if err != nil {
			return err
		}

		if err := checkMayWrite(ctx, tx, chatID, subject, members, current.ThreadID != 0, false); err != nil {
			return err
		}

		if err := current.MayEdit(subject, now); err != nil {
			return err
		}

		clean, err := domain.Revise(text, attachments, entities)
		if err != nil {
			return err
		}

		encoded, err := encodeEntities(entities)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			UPDATE messages SET text = $3, entities = $4, edited_at = $5 WHERE chat_id = $1 AND message_id = $2`,
			chatID, id, clean, encoded, now,
		)
		if err != nil {
			return fmt.Errorf("update message: %w", err)
		}

		members, err = audience(ctx, tx, chatID, current.ThreadID, members)
		if err != nil {
			return err
		}

		return announceMessages(ctx, tx, members, chatID, []int64{id})
	})
	if err != nil {
		return domain.Message{}, nil, fmt.Errorf("edit: %w", err)
	}

	found, err := messagesByRef(ctx, r.pool, []messageRef{{chatID: chatID, id: id}})
	if err != nil {
		return domain.Message{}, nil, err
	}

	if err := attachMineByRef(ctx, r.pool, subject, found); err != nil {
		return domain.Message{}, nil, err
	}

	return found[messageRef{chatID: chatID, id: id}].ForViewer(subject), members, nil
}

func (r *Messages) Delete(
	ctx context.Context,
	chatID string,
	ids []int64,
	subject string,
	forEveryone bool,
	now time.Time,
) ([]int64, []string, error) {
	var (
		removed []int64
		woken   []string
	)

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		all, err := membersOf(ctx, tx, chatID)
		if err != nil {
			return err
		}

		if !slices.Contains(all, subject) {
			return domain.ErrChatNotFound
		}

		if !forEveryone {
			removed, err = hideForSubject(ctx, tx, chatID, ids, subject)
			if err != nil || len(removed) == 0 {
				return err
			}

			woken = []string{subject}

			return announceMessages(ctx, tx, woken, chatID, removed)
		}

		gone, err := deleteForEveryone(ctx, tx, chatID, ids, subject, now)
		if err != nil {
			return err
		}

		removed, woken, err = announceRemoved(ctx, tx, chatID, all, gone)
		if err != nil || len(removed) == 0 {
			return err
		}

		return unpinRemoved(ctx, tx, chatID, removed, all)
	})
	if err != nil {
		return nil, nil, fmt.Errorf("delete: %w", err)
	}

	return removed, woken, nil
}

type removedMessage struct {
	id     int64
	thread int64
}

func announceRemoved(
	ctx context.Context,
	tx pgx.Tx,
	chatID string,
	members []string,
	gone []removedMessage,
) ([]int64, []string, error) {
	var (
		removed []int64
		woken   []string
	)

	byThread := map[int64][]int64{}
	threads := []int64{}

	for _, next := range gone {
		if _, seen := byThread[next.thread]; !seen {
			threads = append(threads, next.thread)
		}

		byThread[next.thread] = append(byThread[next.thread], next.id)
		removed = append(removed, next.id)
	}

	slices.Sort(threads)

	for _, thread := range threads {
		listeners, err := audience(ctx, tx, chatID, thread, members)
		if err != nil {
			return nil, nil, err
		}

		if err := announceMessages(ctx, tx, listeners, chatID, byThread[thread]); err != nil {
			return nil, nil, err
		}

		for _, listener := range listeners {
			if !slices.Contains(woken, listener) {
				woken = append(woken, listener)
			}
		}
	}

	return removed, woken, nil
}

func deleteForEveryone(
	ctx context.Context,
	tx pgx.Tx,
	chatID string,
	ids []int64,
	subject string,
	now time.Time,
) ([]removedMessage, error) {
	access, err := requireAccess(ctx, tx, chatID, subject)
	if err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, `
		SELECT message_id, sender, thread_id FROM messages
		WHERE chat_id = $1 AND message_id = ANY($2) AND deleted_at IS NULL
		ORDER BY message_id FOR UPDATE`, chatID, ids,
	)
	if err != nil {
		return nil, fmt.Errorf("select messages to delete: %w", err)
	}

	type owned struct {
		removedMessage
		sender string
	}

	found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (owned, error) {
		var next owned

		return next, row.Scan(&next.id, &next.sender, &next.thread)
	})
	if err != nil {
		return nil, fmt.Errorf("read messages to delete: %w", err)
	}

	gone := make([]removedMessage, 0, len(found))
	removed := make([]int64, 0, len(found))

	for _, next := range found {
		if !domain.MayDeleteForAll(access, subject, next.sender) {
			return nil, domain.ErrNotAllowed
		}

		gone = append(gone, next.removedMessage)
		removed = append(removed, next.id)
	}

	if len(removed) == 0 {
		return nil, nil
	}

	_, err = tx.Exec(ctx, `
		UPDATE messages SET deleted_at = $3, text = '', attachments = '[]', entities = '[]'
		WHERE chat_id = $1 AND message_id = ANY($2)`, chatID, removed, now,
	)
	if err != nil {
		return nil, fmt.Errorf("delete messages: %w", err)
	}

	return gone, nil
}

func hideForSubject(ctx context.Context, tx pgx.Tx, chatID string, ids []int64, subject string) ([]int64, error) {
	rows, err := tx.Query(ctx, `
		INSERT INTO message_hidden (subject, chat_id, message_id)
		SELECT $3, chat_id, message_id FROM messages WHERE chat_id = $1 AND message_id = ANY($2)
		ON CONFLICT DO NOTHING
		RETURNING message_id`, chatID, ids, subject,
	)
	if err != nil {
		return nil, fmt.Errorf("hide messages: %w", err)
	}

	hidden, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, fmt.Errorf("read hidden messages: %w", err)
	}

	return hidden, nil
}

func lockMessage(ctx context.Context, tx pgx.Tx, chatID string, id int64) (domain.Message, int, error) {
	current := domain.Message{ChatID: chatID, ID: id}

	var (
		raw     []byte
		pollRaw []byte
		deleted bool
	)

	err := tx.QueryRow(ctx, `
		SELECT sender, sent_at, agent_id, forward_name, deleted_at IS NOT NULL, attachments, thread_id, poll
		FROM messages WHERE chat_id = $1 AND message_id = $2 FOR UPDATE`, chatID, id,
	).Scan(&current.Sender, &current.SentAt, &current.AgentID, &current.ForwardName, &deleted, &raw, &current.ThreadID, &pollRaw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Message{}, 0, domain.ErrMessageMissing
		}

		return domain.Message{}, 0, fmt.Errorf("lock message: %w", err)
	}

	current.IsDeleted = deleted

	current.Poll, err = decodePoll(pollRaw)
	if err != nil {
		return domain.Message{}, 0, err
	}

	attachments, err := decodeAttachments(raw)
	if err != nil {
		return domain.Message{}, 0, err
	}

	return current, len(attachments), nil
}

func announceMessages(ctx context.Context, tx pgx.Tx, members []string, chatID string, ids []int64) error {
	for _, id := range ids {
		for _, member := range members {
			if err := appendMessageUpdate(ctx, tx, member, chatID, id); err != nil {
				return err
			}
		}
	}

	for _, member := range members {
		if err := appendUpdate(ctx, tx, member, update.KindChats); err != nil {
			return err
		}
	}

	return nil
}

func hiddenFor(ctx context.Context, pool *pgxpool.Pool, subject string, refs []messageRef) (map[messageRef]bool, error) {
	hidden := make(map[messageRef]bool, len(refs))
	if len(refs) == 0 {
		return hidden, nil
	}

	chatIDs := make([]string, len(refs))
	ids := make([]int64, len(refs))

	for index, ref := range refs {
		chatIDs[index] = ref.chatID
		ids[index] = ref.id
	}

	rows, err := pool.Query(ctx, `
		SELECT chat_id, message_id FROM message_hidden
		WHERE subject = $1 AND (chat_id, message_id) IN (SELECT * FROM unnest($2::text[], $3::bigint[]))`,
		subject, chatIDs, ids,
	)
	if err != nil {
		return nil, fmt.Errorf("select hidden messages: %w", err)
	}

	found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (messageRef, error) {
		var ref messageRef

		return ref, row.Scan(&ref.chatID, &ref.id)
	})
	if err != nil {
		return nil, fmt.Errorf("read hidden messages: %w", err)
	}

	for _, ref := range found {
		hidden[ref] = true
	}

	return hidden, nil
}
