package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	domain "go.airlance.org/server/internal/domain/message"
)

func (r *Messages) Readers(
	ctx context.Context,
	chatID, subject string,
	messageID int64,
	limit int,
) ([]domain.Member, error) {
	var found []domain.Member

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		access, err := requireAccess(ctx, tx, chatID, subject)
		if err != nil {
			return err
		}

		if access.Kind != domain.KindGroup {
			return domain.ErrNotAllowed
		}

		var sender string

		err = tx.QueryRow(ctx, `SELECT sender FROM messages WHERE chat_id = $1 AND message_id = $2 LIMIT 1`,
			chatID, messageID).Scan(&sender)
		if err != nil {
			return domain.ErrMessageMissing
		}

		if sender != subject {
			return domain.ErrNotAllowed
		}

		rows, err := tx.Query(ctx, `
			SELECT m.subject, COALESCE(p.first_name, ''), COALESCE(p.last_name, ''), COALESCE(p.username, ''),
				CASE WHEN COALESCE(q.photos, 0) = 0 THEN COALESCE(p.photo_media_id, '') ELSE '' END,
				CASE WHEN COALESCE(q.photos, 0) = 0 THEN COALESCE(p.video_media_id, '') ELSE '' END
			FROM chat_members m
			LEFT JOIN profiles p ON p.subject = m.subject
			LEFT JOIN profile_privacy q ON q.subject = m.subject
			WHERE m.chat_id = $1 AND m.subject <> $2 AND m.read_up_to >= $3
			ORDER BY m.subject LIMIT $4`, chatID, subject, messageID, limit,
		)
		if err != nil {
			return fmt.Errorf("select readers: %w", err)
		}

		found, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Member, error) {
			var next domain.Member

			peer := &next.Peer

			return next, row.Scan(&peer.Subject, &peer.FirstName, &peer.LastName, &peer.Username,
				&peer.PhotoMediaID, &peer.VideoMediaID)
		})

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("read readers: %w", err)
	}

	return found, nil
}

func (r *Messages) TypingAudience(ctx context.Context, chatID, subject string) (domain.Peer, []string, error) {
	var (
		who      domain.Peer
		audience []string
	)

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		members, err := membersOf(ctx, tx, chatID)
		if err != nil {
			return err
		}

		if !slices.Contains(members, subject) {
			return domain.ErrChatNotFound
		}

		access, err := requireAccess(ctx, tx, chatID, subject)
		if err != nil {
			return err
		}

		if access.Kind == domain.KindChannel || !access.Can(domain.RightSend) {
			return nil
		}

		who.Subject = subject

		err = tx.QueryRow(ctx, `
			SELECT COALESCE(first_name, ''), COALESCE(last_name, ''), COALESCE(username, '')
			FROM profiles WHERE subject = $1`, subject).Scan(&who.FirstName, &who.LastName, &who.Username)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("select typist: %w", err)
		}

		for _, member := range members {
			if member != subject {
				audience = append(audience, member)
			}
		}

		return nil
	})
	if err != nil {
		return domain.Peer{}, nil, fmt.Errorf("typing audience: %w", err)
	}

	return who, audience, nil
}
