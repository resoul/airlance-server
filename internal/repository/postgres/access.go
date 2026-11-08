package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	domain "go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/update"
)

func accessIn(ctx context.Context, tx pgx.Tx, chatID, subject string) (domain.Access, bool, error) {
	var access domain.Access

	err := tx.QueryRow(ctx, `
		SELECT c.kind, m.role, m.rights, c.member_rights FROM chats c
		JOIN chat_members m ON m.chat_id = c.id AND m.subject = $2
		WHERE c.id = $1`, chatID, subject,
	).Scan(&access.Kind, &access.Role, &access.Admin, &access.Member)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Access{}, false, nil
	}

	if err != nil {
		return domain.Access{}, false, fmt.Errorf("select access: %w", err)
	}

	return access, true, nil
}

func requireAccess(ctx context.Context, tx pgx.Tx, chatID, subject string) (domain.Access, error) {
	access, isMember, err := accessIn(ctx, tx, chatID, subject)
	if err != nil {
		return domain.Access{}, err
	}

	if !isMember {
		return domain.Access{}, domain.ErrChatNotFound
	}

	return access, nil
}

func isBanned(ctx context.Context, tx pgx.Tx, chatID, subject string) (bool, error) {
	var found bool

	err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM chat_bans WHERE chat_id = $1 AND subject = $2)`, chatID, subject,
	).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("check ban: %w", err)
	}

	return found, nil
}

func (r *Messages) SetMemberRights(ctx context.Context, chatID, actor string, rights domain.Rights) ([]string, error) {
	var woken []string

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := lockChat(ctx, tx, chatID); err != nil {
			return err
		}

		access, err := requireAccess(ctx, tx, chatID, actor)
		if err != nil {
			return err
		}

		if access.Kind != domain.KindGroup || !access.Can(domain.RightBan) {
			return domain.ErrNotAllowed
		}

		_, err = tx.Exec(ctx, `UPDATE chats SET member_rights = $2 WHERE id = $1`, chatID, domain.CleanMemberRights(rights))
		if err != nil {
			return fmt.Errorf("update member rights: %w", err)
		}

		woken, err = membersOf(ctx, tx, chatID)
		if err != nil {
			return err
		}

		for _, member := range woken {
			if err := appendUpdate(ctx, tx, member, update.KindChats); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("set member rights: %w", err)
	}

	return woken, nil
}

func (r *Messages) Bans(ctx context.Context, chatID, actor string, limit int) ([]domain.Member, error) {
	var found []domain.Member

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		access, err := requireAccess(ctx, tx, chatID, actor)
		if err != nil {
			return err
		}

		if !access.Can(domain.RightBan) {
			return domain.ErrNotAllowed
		}

		rows, err := tx.Query(ctx, `
			SELECT b.subject, COALESCE(p.first_name, ''), COALESCE(p.last_name, ''), COALESCE(p.username, ''),
				COALESCE(p.photo_media_id, ''), COALESCE(p.video_media_id, '')
			FROM chat_bans b LEFT JOIN profiles p ON p.subject = b.subject
			WHERE b.chat_id = $1 ORDER BY b.banned_at DESC, b.subject LIMIT $2`, chatID, limit,
		)
		if err != nil {
			return fmt.Errorf("select bans: %w", err)
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
		return nil, fmt.Errorf("read bans: %w", err)
	}

	return found, nil
}

func (r *Messages) Unban(ctx context.Context, chatID, actor, subject string) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		access, err := requireAccess(ctx, tx, chatID, actor)
		if err != nil {
			return err
		}

		if !access.Can(domain.RightBan) {
			return domain.ErrNotAllowed
		}

		tag, err := tx.Exec(ctx, `DELETE FROM chat_bans WHERE chat_id = $1 AND subject = $2`, chatID, subject)
		if err != nil {
			return fmt.Errorf("delete ban: %w", err)
		}

		if tag.RowsAffected() == 0 {
			return nil
		}

		return insertLog(ctx, tx, chatID, actor, subject, domain.LogEntry{Kind: domain.LogUnbanned})
	})
	if err != nil {
		return fmt.Errorf("unban: %w", err)
	}

	return nil
}

func insertBan(ctx context.Context, tx pgx.Tx, chatID, subject, by string, now time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO chat_bans (chat_id, subject, banned_by, banned_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT (chat_id, subject) DO NOTHING`, chatID, subject, by, now,
	)
	if err != nil {
		return fmt.Errorf("insert ban: %w", err)
	}

	return nil
}

func (r *Messages) SetInfo(
	ctx context.Context,
	chatID, actor string,
	info domain.ChatInfo,
	token string,
) ([]string, error) {
	var woken []string

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		kind, err := lockChat(ctx, tx, chatID)
		if err != nil {
			return err
		}

		access, err := requireAccess(ctx, tx, chatID, actor)
		if err != nil {
			return err
		}

		if kind == domain.KindDirect || !access.Can(domain.RightInfo) {
			return domain.ErrNotAllowed
		}

		var before domain.ChatInfo

		err = tx.QueryRow(ctx, `SELECT COALESCE(title, ''), COALESCE(about, ''), COALESCE(link, '') FROM chats WHERE id = $1`,
			chatID).Scan(&before.Title, &before.About, &before.Link)
		if err != nil {
			return fmt.Errorf("select info: %w", err)
		}

		_, err = tx.Exec(ctx, `
			UPDATE chats SET title = $2, about = $3, link = NULLIF($4, ''),
				invite_token = CASE WHEN $4 <> '' THEN NULL ELSE COALESCE(invite_token, $5) END
			WHERE id = $1`, chatID, info.Title, info.About, info.Link, token,
		)
		if err != nil {
			return chatInsertError(err)
		}

		if err := insertLogs(ctx, tx, chatID, actor, domain.InfoChanges(before, info)); err != nil {
			return err
		}

		woken, err = membersOf(ctx, tx, chatID)
		if err != nil {
			return err
		}

		for _, member := range woken {
			if err := appendUpdate(ctx, tx, member, update.KindChats); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("set info: %w", err)
	}

	return woken, nil
}

func (r *Messages) SetRules(ctx context.Context, chatID, actor string, rules domain.ChatRules) ([]string, error) {
	var woken []string

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		kind, err := lockChat(ctx, tx, chatID)
		if err != nil {
			return err
		}

		access, err := requireAccess(ctx, tx, chatID, actor)
		if err != nil {
			return err
		}

		if kind == domain.KindDirect || !access.Can(domain.RightInfo) {
			return domain.ErrNotAllowed
		}

		var before domain.ChatRules

		err = tx.QueryRow(ctx, `
			SELECT no_copy, signs, reactions_mode, reactions, reactions_per_post FROM chats WHERE id = $1`, chatID,
		).Scan(&before.NoCopy, &before.Signs, &before.ReactionsMode, &before.Reactions, &before.PerPost)
		if err != nil {
			return fmt.Errorf("select rules: %w", err)
		}

		reactions := rules.Reactions
		if reactions == nil {
			reactions = []string{}
		}

		_, err = tx.Exec(ctx, `
			UPDATE chats SET no_copy = $2, signs = $3, reactions_mode = $4, reactions = $5, reactions_per_post = $6
			WHERE id = $1`, chatID, rules.NoCopy, rules.Signs, rules.ReactionsMode, reactions, rules.PerPost,
		)
		if err != nil {
			return fmt.Errorf("update rules: %w", err)
		}

		if err := insertLogs(ctx, tx, chatID, actor, kindEntries(before.Changes(rules))); err != nil {
			return err
		}

		woken, err = membersOf(ctx, tx, chatID)
		if err != nil {
			return err
		}

		for _, member := range woken {
			if err := appendUpdate(ctx, tx, member, update.KindChats); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("set rules: %w", err)
	}

	return woken, nil
}
