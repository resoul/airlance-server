package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	domain "go.airlance.org/server/internal/domain/message"
)

type reactionRow struct {
	Emoji string `json:"e"`
	Count int32  `json:"n"`
}

func decodeReactions(raw []byte) ([]domain.ReactionCount, error) {
	var rows []reactionRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("decode reactions: %w", err)
	}

	if len(rows) == 0 {
		return nil, nil
	}

	counts := make([]domain.ReactionCount, len(rows))
	for index, next := range rows {
		counts[index] = domain.ReactionCount{Emoji: next.Emoji, Count: next.Count}
	}

	return counts, nil
}

func attachMine(ctx context.Context, db querier, subject string, messages []domain.Message) error {
	if len(messages) == 0 {
		return nil
	}

	chatIDs := make([]string, len(messages))
	ids := make([]int64, len(messages))

	for index, next := range messages {
		chatIDs[index] = next.ChatID
		ids[index] = next.ID
	}

	mine, err := myReactions(ctx, db, subject, chatIDs, ids)
	if err != nil {
		return err
	}

	var polls []pollItem

	for index := range messages {
		ref := messageRef{chatID: messages[index].ChatID, id: messages[index].ID}
		messages[index].MyReaction = mine[ref]

		if messages[index].Poll != nil {
			polls = append(polls, pollItem{ref: ref, sender: messages[index].Sender, poll: messages[index].Poll})
		}
	}

	return fillPolls(ctx, db, subject, polls)
}

func attachMineByRef(ctx context.Context, db querier, subject string, found map[messageRef]domain.Message) error {
	if len(found) == 0 {
		return nil
	}

	chatIDs := make([]string, 0, len(found))
	ids := make([]int64, 0, len(found))

	for ref := range found {
		chatIDs = append(chatIDs, ref.chatID)
		ids = append(ids, ref.id)
	}

	mine, err := myReactions(ctx, db, subject, chatIDs, ids)
	if err != nil {
		return err
	}

	for ref, emoji := range mine {
		next := found[ref]
		next.MyReaction = emoji
		found[ref] = next
	}

	var polls []pollItem

	for ref, next := range found {
		if next.Poll != nil {
			polls = append(polls, pollItem{ref: ref, sender: next.Sender, poll: next.Poll})
		}
	}

	return fillPolls(ctx, db, subject, polls)
}

func myReactions(
	ctx context.Context,
	db querier,
	subject string,
	chatIDs []string,
	ids []int64,
) (map[messageRef]string, error) {
	rows, err := db.Query(ctx, `
		SELECT chat_id, message_id, emoji FROM message_reactions
		WHERE subject = $1 AND (chat_id, message_id) IN (SELECT * FROM unnest($2::text[], $3::bigint[]))`,
		subject, chatIDs, ids,
	)
	if err != nil {
		return nil, fmt.Errorf("select own reactions: %w", err)
	}

	defer rows.Close()

	mine := map[messageRef]string{}

	for rows.Next() {
		var (
			ref   messageRef
			emoji string
		)

		if err := rows.Scan(&ref.chatID, &ref.id, &emoji); err != nil {
			return nil, fmt.Errorf("scan own reaction: %w", err)
		}

		mine[ref] = emoji
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read own reactions: %w", err)
	}

	return mine, nil
}

func (r *Messages) SetReaction(
	ctx context.Context,
	chatID string,
	id int64,
	subject, emoji string,
	now time.Time,
) (domain.Message, []string, error) {
	var audienceOf []string

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		members, err := membersOf(ctx, tx, chatID)
		if err != nil {
			return err
		}

		if !slices.Contains(members, subject) {
			return domain.ErrChatNotFound
		}

		current, _, err := lockMessage(ctx, tx, chatID, id)
		if err != nil {
			return err
		}

		if current.IsDeleted {
			return domain.ErrMessageMissing
		}

		if err := checkReactionRules(ctx, tx, chatID, id, subject, emoji); err != nil {
			return err
		}

		if err := writeReaction(ctx, tx, chatID, id, subject, emoji, now); err != nil {
			return err
		}

		audienceOf, err = reactionAudience(ctx, tx, chatID, current.ThreadID, subject, members)
		if err != nil {
			return err
		}

		for _, member := range audienceOf {
			if err := appendMessageUpdate(ctx, tx, member, chatID, id); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return domain.Message{}, nil, fmt.Errorf("set reaction: %w", err)
	}

	found, err := messagesByRef(ctx, r.pool, []messageRef{{chatID: chatID, id: id}})
	if err != nil {
		return domain.Message{}, nil, err
	}

	if err := attachMineByRef(ctx, r.pool, subject, found); err != nil {
		return domain.Message{}, nil, err
	}

	return found[messageRef{chatID: chatID, id: id}].ForViewer(subject), audienceOf, nil
}

func checkReactionRules(ctx context.Context, tx pgx.Tx, chatID string, id int64, subject, emoji string) error {
	if emoji == "" {
		return nil
	}

	var rules domain.ChatRules

	err := tx.QueryRow(ctx, `SELECT reactions_mode, reactions, reactions_per_post FROM chats WHERE id = $1`, chatID).
		Scan(&rules.ReactionsMode, &rules.Reactions, &rules.PerPost)
	if err != nil {
		return fmt.Errorf("select reaction rules: %w", err)
	}

	if !rules.AllowsReaction(emoji) {
		return domain.ErrNotAllowed
	}

	if rules.PerPost == 0 {
		return nil
	}

	var (
		kinds int
		known bool
	)

	err = tx.QueryRow(ctx, `
		SELECT count(DISTINCT emoji), COALESCE(bool_or(emoji = $3), false) FROM message_reactions
		WHERE chat_id = $1 AND message_id = $2 AND subject <> $4`, chatID, id, emoji, subject,
	).Scan(&kinds, &known)
	if err != nil {
		return fmt.Errorf("count reaction kinds: %w", err)
	}

	if !known && kinds >= int(rules.PerPost) {
		return domain.ErrNotAllowed
	}

	return nil
}

func writeReaction(ctx context.Context, tx pgx.Tx, chatID string, id int64, subject, emoji string, now time.Time) error {
	if emoji == "" {
		_, err := tx.Exec(ctx, `
			DELETE FROM message_reactions WHERE chat_id = $1 AND message_id = $2 AND subject = $3`,
			chatID, id, subject,
		)
		if err != nil {
			return fmt.Errorf("delete reaction: %w", err)
		}

		return nil
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO message_reactions (chat_id, message_id, subject, emoji, reacted_at) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (chat_id, message_id, subject) DO UPDATE SET emoji = excluded.emoji, reacted_at = excluded.reacted_at`,
		chatID, id, subject, emoji, now,
	)
	if err != nil {
		return fmt.Errorf("upsert reaction: %w", err)
	}

	return nil
}

func reactionAudience(
	ctx context.Context,
	tx pgx.Tx,
	chatID string,
	thread int64,
	subject string,
	members []string,
) ([]string, error) {
	var kind domain.Kind

	if err := tx.QueryRow(ctx, `SELECT kind FROM chats WHERE id = $1`, chatID).Scan(&kind); err != nil {
		return nil, fmt.Errorf("select chat kind: %w", err)
	}

	if kind == domain.KindChannel {
		return []string{subject}, nil
	}

	return audience(ctx, tx, chatID, thread, members)
}
