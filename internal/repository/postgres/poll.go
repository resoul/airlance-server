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

type pollRow struct {
	Question    string     `json:"q"`
	Options     []string   `json:"o"`
	Anonymous   bool       `json:"anon,omitempty"`
	Multiple    bool       `json:"multi,omitempty"`
	Quiz        bool       `json:"quiz,omitempty"`
	Correct     int32      `json:"correct"`
	Explanation string     `json:"why,omitempty"`
	CloseAt     *time.Time `json:"close_at,omitempty"`
	ClosedAt    *time.Time `json:"closed_at,omitempty"`
}

func encodePoll(poll *domain.Poll) ([]byte, error) {
	if poll == nil {
		return nil, nil
	}

	row := pollRow{
		Question: poll.Question, Anonymous: poll.Anonymous, Multiple: poll.Multiple, Quiz: poll.Quiz,
		Correct: poll.Correct, Explanation: poll.Explanation,
	}

	for _, next := range poll.Options {
		row.Options = append(row.Options, next.Text)
	}

	if !poll.CloseAt.IsZero() {
		row.CloseAt = &poll.CloseAt
	}

	encoded, err := json.Marshal(row)
	if err != nil {
		return nil, fmt.Errorf("encode poll: %w", err)
	}

	return encoded, nil
}

func decodePoll(raw []byte) (*domain.Poll, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var row pollRow
	if err := json.Unmarshal(raw, &row); err != nil {
		return nil, fmt.Errorf("decode poll: %w", err)
	}

	poll := domain.Poll{
		Question: row.Question, Anonymous: row.Anonymous, Multiple: row.Multiple, Quiz: row.Quiz,
		Correct: row.Correct, Explanation: row.Explanation,
	}

	for _, text := range row.Options {
		poll.Options = append(poll.Options, domain.PollOption{Text: text})
	}

	if row.CloseAt != nil {
		poll.CloseAt = *row.CloseAt
	}

	if row.ClosedAt != nil {
		poll.ClosedAt = *row.ClosedAt
	}

	return &poll, nil
}

type pollItem struct {
	ref    messageRef
	sender string
	poll   *domain.Poll
}

func fillPolls(ctx context.Context, db querier, subject string, items []pollItem) error {
	if len(items) == 0 {
		return nil
	}

	chatIDs := make([]string, len(items))
	ids := make([]int64, len(items))

	for index, next := range items {
		chatIDs[index], ids[index] = next.ref.chatID, next.ref.id
		next.poll.IsClosed = next.poll.Finished(time.Now())
		next.poll.Mine, next.poll.TotalVoters = nil, 0

		for option := range next.poll.Options {
			next.poll.Options[option].Votes, next.poll.Options[option].Voters = 0, nil
		}
	}

	byRef := make(map[messageRef]*domain.Poll, len(items))
	for _, next := range items {
		byRef[next.ref] = next.poll
	}

	if err := fillTallies(ctx, db, byRef, chatIDs, ids); err != nil {
		return err
	}

	if err := fillMine(ctx, db, subject, byRef, chatIDs, ids); err != nil {
		return err
	}

	hideAnswers(subject, items)

	return fillVoters(ctx, db, items, byRef)
}

func hideAnswers(subject string, items []pollItem) {
	for _, next := range items {
		poll := next.poll
		if !poll.Quiz || poll.IsClosed || len(poll.Mine) > 0 || next.sender == subject {
			continue
		}

		poll.Correct, poll.Explanation = -1, ""
	}
}

func fillTallies(
	ctx context.Context,
	db querier,
	byRef map[messageRef]*domain.Poll,
	chatIDs []string,
	ids []int64,
) error {
	rows, err := db.Query(ctx, `
		SELECT chat_id, message_id, option, count(*) FROM poll_votes
		WHERE (chat_id, message_id) IN (SELECT * FROM unnest($1::text[], $2::bigint[]))
		GROUP BY chat_id, message_id, option`, chatIDs, ids,
	)
	if err != nil {
		return fmt.Errorf("select poll tallies: %w", err)
	}

	for rows.Next() {
		var (
			ref    messageRef
			option int16
			votes  int32
		)

		if err := rows.Scan(&ref.chatID, &ref.id, &option, &votes); err != nil {
			rows.Close()

			return fmt.Errorf("scan poll tally: %w", err)
		}

		if poll := byRef[ref]; poll != nil && int(option) < len(poll.Options) {
			poll.Options[option].Votes = votes
		}
	}

	rows.Close()

	if err := rows.Err(); err != nil {
		return fmt.Errorf("read poll tallies: %w", err)
	}

	return fillTotals(ctx, db, byRef, chatIDs, ids)
}

func fillTotals(
	ctx context.Context,
	db querier,
	byRef map[messageRef]*domain.Poll,
	chatIDs []string,
	ids []int64,
) error {
	rows, err := db.Query(ctx, `
		SELECT chat_id, message_id, count(DISTINCT subject) FROM poll_votes
		WHERE (chat_id, message_id) IN (SELECT * FROM unnest($1::text[], $2::bigint[]))
		GROUP BY chat_id, message_id`, chatIDs, ids,
	)
	if err != nil {
		return fmt.Errorf("select poll totals: %w", err)
	}

	defer rows.Close()

	for rows.Next() {
		var (
			ref   messageRef
			total int32
		)

		if err := rows.Scan(&ref.chatID, &ref.id, &total); err != nil {
			return fmt.Errorf("scan poll total: %w", err)
		}

		if poll := byRef[ref]; poll != nil {
			poll.TotalVoters = total
		}
	}

	return rows.Err()
}

func fillMine(
	ctx context.Context,
	db querier,
	subject string,
	byRef map[messageRef]*domain.Poll,
	chatIDs []string,
	ids []int64,
) error {
	rows, err := db.Query(ctx, `
		SELECT chat_id, message_id, option FROM poll_votes
		WHERE subject = $1 AND (chat_id, message_id) IN (SELECT * FROM unnest($2::text[], $3::bigint[]))
		ORDER BY option`, subject, chatIDs, ids,
	)
	if err != nil {
		return fmt.Errorf("select own votes: %w", err)
	}

	defer rows.Close()

	for rows.Next() {
		var (
			ref    messageRef
			option int16
		)

		if err := rows.Scan(&ref.chatID, &ref.id, &option); err != nil {
			return fmt.Errorf("scan own vote: %w", err)
		}

		if poll := byRef[ref]; poll != nil {
			poll.Mine = append(poll.Mine, int32(option))
		}
	}

	return rows.Err()
}

func fillVoters(ctx context.Context, db querier, items []pollItem, byRef map[messageRef]*domain.Poll) error {
	var (
		chatIDs []string
		ids     []int64
	)

	for _, next := range items {
		if !next.poll.Anonymous {
			chatIDs = append(chatIDs, next.ref.chatID)
			ids = append(ids, next.ref.id)
		}
	}

	if len(ids) == 0 {
		return nil
	}

	rows, err := db.Query(ctx, `
		SELECT v.chat_id, v.message_id, v.option, v.subject, COALESCE(p.first_name, ''), COALESCE(p.last_name, ''),
			CASE WHEN COALESCE(q.photos, 0) = 0 THEN COALESCE(p.photo_media_id, '') ELSE '' END,
			CASE WHEN COALESCE(q.photos, 0) = 0 THEN COALESCE(p.video_media_id, '') ELSE '' END
		FROM (
			SELECT *, row_number() OVER (PARTITION BY chat_id, message_id, option ORDER BY voted_at DESC) AS place
			FROM poll_votes
			WHERE (chat_id, message_id) IN (SELECT * FROM unnest($1::text[], $2::bigint[]))
		) v
		LEFT JOIN profiles p ON p.subject = v.subject
		LEFT JOIN profile_privacy q ON q.subject = v.subject
		WHERE v.place <= $3
		ORDER BY v.chat_id, v.message_id, v.option, v.place`, chatIDs, ids, domain.MaxPollVoters,
	)
	if err != nil {
		return fmt.Errorf("select poll voters: %w", err)
	}

	defer rows.Close()

	for rows.Next() {
		var (
			ref    messageRef
			option int16
			voter  domain.Commenter
		)

		if err := rows.Scan(
			&ref.chatID, &ref.id, &option, &voter.Subject, &voter.FirstName, &voter.LastName,
			&voter.PhotoMediaID, &voter.VideoMediaID,
		); err != nil {
			return fmt.Errorf("scan poll voter: %w", err)
		}

		if poll := byRef[ref]; poll != nil && int(option) < len(poll.Options) {
			poll.Options[option].Voters = append(poll.Options[option].Voters, voter)
		}
	}

	return rows.Err()
}

func (r *Messages) Vote(
	ctx context.Context,
	chatID string,
	id int64,
	subject string,
	options []int32,
	now time.Time,
) (domain.Message, []string, error) {
	var woken []string

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

		if err := checkVotable(current, options, now); err != nil {
			return err
		}

		if err := replaceVotes(ctx, tx, chatID, id, subject, options, current.Poll.Quiz, now); err != nil {
			return err
		}

		woken, err = reactionAudience(ctx, tx, chatID, current.ThreadID, subject, members)
		if err != nil {
			return err
		}

		return announceUpdates(ctx, tx, woken, chatID, id)
	})
	if err != nil {
		return domain.Message{}, nil, fmt.Errorf("vote: %w", err)
	}

	return r.fresh(ctx, chatID, id, subject, woken)
}

func checkVotable(current domain.Message, options []int32, now time.Time) error {
	if current.IsDeleted || current.Poll == nil {
		return domain.ErrMessageMissing
	}

	if current.Poll.Finished(now) {
		return domain.ErrPollClosed
	}

	return current.Poll.CheckVote(options)
}

func replaceVotes(
	ctx context.Context,
	tx pgx.Tx,
	chatID string,
	id int64,
	subject string,
	options []int32,
	isQuiz bool,
	now time.Time,
) error {
	var voted int

	err := tx.QueryRow(ctx, `
		SELECT count(*) FROM poll_votes WHERE chat_id = $1 AND message_id = $2 AND subject = $3`,
		chatID, id, subject,
	).Scan(&voted)
	if err != nil {
		return fmt.Errorf("count own votes: %w", err)
	}

	if isQuiz && voted > 0 {
		return domain.ErrAlreadyVoted
	}

	_, err = tx.Exec(ctx, `DELETE FROM poll_votes WHERE chat_id = $1 AND message_id = $2 AND subject = $3`,
		chatID, id, subject)
	if err != nil {
		return fmt.Errorf("delete votes: %w", err)
	}

	for _, option := range options {
		_, err = tx.Exec(ctx, `
			INSERT INTO poll_votes (chat_id, message_id, subject, option, voted_at) VALUES ($1, $2, $3, $4, $5)`,
			chatID, id, subject, int16(option), now,
		)
		if err != nil {
			return fmt.Errorf("insert vote: %w", err)
		}
	}

	return nil
}

func (r *Messages) ClosePoll(
	ctx context.Context,
	chatID string,
	id int64,
	subject string,
	now time.Time,
) (domain.Message, []string, error) {
	var woken []string

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

		if current.IsDeleted || current.Poll == nil {
			return domain.ErrMessageMissing
		}

		if current.Sender != subject {
			return domain.ErrNotAllowed
		}

		_, err = tx.Exec(ctx, `
			UPDATE messages SET poll = jsonb_set(poll, '{closed_at}', to_jsonb($3::text))
			WHERE chat_id = $1 AND message_id = $2 AND NOT (poll ? 'closed_at')`,
			chatID, id, now.UTC().Format(time.RFC3339Nano),
		)
		if err != nil {
			return fmt.Errorf("close poll: %w", err)
		}

		woken, err = audience(ctx, tx, chatID, current.ThreadID, members)
		if err != nil {
			return err
		}

		return announceUpdates(ctx, tx, woken, chatID, id)
	})
	if err != nil {
		return domain.Message{}, nil, fmt.Errorf("close poll: %w", err)
	}

	return r.fresh(ctx, chatID, id, subject, woken)
}

func announceUpdates(ctx context.Context, tx pgx.Tx, members []string, chatID string, id int64) error {
	for _, member := range members {
		if err := appendMessageUpdate(ctx, tx, member, chatID, id); err != nil {
			return err
		}
	}

	return nil
}

func (r *Messages) fresh(
	ctx context.Context,
	chatID string,
	id int64,
	subject string,
	woken []string,
) (domain.Message, []string, error) {
	found, err := messagesByRef(ctx, r.pool, []messageRef{{chatID: chatID, id: id}})
	if err != nil {
		return domain.Message{}, nil, err
	}

	if err := attachMineByRef(ctx, r.pool, subject, found); err != nil {
		return domain.Message{}, nil, err
	}

	return found[messageRef{chatID: chatID, id: id}].ForViewer(subject), woken, nil
}
