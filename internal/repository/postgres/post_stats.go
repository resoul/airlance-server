package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	domain "go.airlance.org/server/internal/domain/message"
)

func (r *Messages) PostStats(
	ctx context.Context,
	chatID, subject string,
	ids []int64,
	markViewed bool,
) ([]domain.PostStat, error) {
	var (
		mayRead bool
		kind    domain.Kind
	)

	err := r.pool.QueryRow(ctx, `
		SELECT c.kind,
			EXISTS (SELECT 1 FROM chat_members WHERE chat_id = c.id AND subject = $2) OR c.link IS NOT NULL
		FROM chats c WHERE c.id = $1`, chatID, subject,
	).Scan(&kind, &mayRead)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("check access: %w", err)
	}

	if !mayRead {
		return nil, domain.ErrChatNotFound
	}

	if kind != domain.KindChannel {
		return nil, nil
	}

	if markViewed {
		if err := r.markViewed(ctx, chatID, subject, ids); err != nil {
			return nil, err
		}
	}

	stats, err := r.postCounts(ctx, chatID, ids)
	if err != nil {
		return nil, err
	}

	if err := r.attachComments(ctx, chatID, stats); err != nil {
		return nil, err
	}

	result := make([]domain.PostStat, 0, len(stats))
	for _, id := range ids {
		if next, ok := stats[id]; ok {
			result = append(result, *next)
			delete(stats, id)
		}
	}

	return result, nil
}

func (r *Messages) markViewed(ctx context.Context, chatID, subject string, ids []int64) error {
	_, err := r.pool.Exec(ctx, `
		WITH fresh AS (
			INSERT INTO message_views (chat_id, message_id, viewer)
			SELECT chat_id, message_id, $3 FROM messages
			WHERE chat_id = $1 AND message_id = ANY($2) AND thread_id = 0 AND deleted_at IS NULL
			ON CONFLICT DO NOTHING
			RETURNING message_id
		)
		UPDATE messages SET views = views + 1
		WHERE chat_id = $1 AND message_id IN (SELECT message_id FROM fresh)`, chatID, ids, subject,
	)
	if err != nil {
		return fmt.Errorf("mark viewed: %w", err)
	}

	return nil
}

func (r *Messages) postCounts(ctx context.Context, chatID string, ids []int64) (map[int64]*domain.PostStat, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT message_id, views FROM messages
		WHERE chat_id = $1 AND message_id = ANY($2) AND thread_id = 0 AND deleted_at IS NULL`, chatID, ids,
	)
	if err != nil {
		return nil, fmt.Errorf("select views: %w", err)
	}

	found, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.PostStat, error) {
		var next domain.PostStat

		return next, row.Scan(&next.MessageID, &next.Views)
	})
	if err != nil {
		return nil, fmt.Errorf("read views: %w", err)
	}

	stats := make(map[int64]*domain.PostStat, len(found))

	for index := range found {
		stats[found[index].MessageID] = &found[index]
	}

	return stats, nil
}

func (r *Messages) attachComments(ctx context.Context, chatID string, stats map[int64]*domain.PostStat) error {
	if len(stats) == 0 {
		return nil
	}

	threads := make([]int64, 0, len(stats))
	for id := range stats {
		threads = append(threads, id)
	}

	counts, err := r.pool.Query(ctx, `
		SELECT thread_id, count(*) FROM messages
		WHERE chat_id = $1 AND thread_id = ANY($2) AND deleted_at IS NULL
		GROUP BY thread_id`, chatID, threads,
	)
	if err != nil {
		return fmt.Errorf("select comment counts: %w", err)
	}

	defer counts.Close()

	for counts.Next() {
		var (
			thread int64
			total  int32
		)

		if err := counts.Scan(&thread, &total); err != nil {
			return fmt.Errorf("scan comment counts: %w", err)
		}

		stats[thread].Comments = total
	}

	if err := counts.Err(); err != nil {
		return fmt.Errorf("read comment counts: %w", err)
	}

	return r.attachRecent(ctx, chatID, threads, stats)
}

func (r *Messages) attachRecent(
	ctx context.Context,
	chatID string,
	threads []int64,
	stats map[int64]*domain.PostStat,
) error {
	rows, err := r.pool.Query(ctx, `
		SELECT m.thread_id, m.sender, COALESCE(p.first_name, ''), COALESCE(p.last_name, ''),
			CASE WHEN COALESCE(q.photos, 0) = 0 THEN COALESCE(p.photo_media_id, '') ELSE '' END,
			CASE WHEN COALESCE(q.photos, 0) = 0 THEN COALESCE(p.video_media_id, '') ELSE '' END
		FROM (
			SELECT thread_id, sender, max(message_id) AS last FROM messages
			WHERE chat_id = $1 AND thread_id = ANY($2) AND deleted_at IS NULL
			GROUP BY thread_id, sender
		) m
		LEFT JOIN profiles p ON p.subject = m.sender
		LEFT JOIN profile_privacy q ON q.subject = m.sender
		ORDER BY m.thread_id, m.last DESC`, chatID, threads,
	)
	if err != nil {
		return fmt.Errorf("select recent commenters: %w", err)
	}

	defer rows.Close()

	for rows.Next() {
		var (
			thread int64
			next   domain.Commenter
		)

		if err := rows.Scan(
			&thread, &next.Subject, &next.FirstName, &next.LastName, &next.PhotoMediaID, &next.VideoMediaID,
		); err != nil {
			return fmt.Errorf("scan recent commenters: %w", err)
		}

		if stat := stats[thread]; len(stat.Recent) < domain.MaxRecentAuthors {
			stat.Recent = append(stat.Recent, next)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("read recent commenters: %w", err)
	}

	return nil
}
