package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"go.airlance.org/server/internal/domain/agent"
	"go.airlance.org/server/internal/domain/directory"
	domain "go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/update"
)

const linkConstraint = "chats_link_key"

const chatSelect = `
	SELECT c.id, c.kind, c.title, c.about, COALESCE(c.link, ''),
		CASE WHEN COALESCE(m.role, 0) >= 1 THEN COALESCE(c.invite_token, '') ELSE '' END,
		c.member_count, COALESCE(m.role, 0), m.subject IS NOT NULL,
		COALESCE((
			SELECT x.message_id FROM messages x
			WHERE x.chat_id = c.id AND x.deleted_at IS NULL AND x.thread_id = 0 AND NOT EXISTS (
				SELECT 1 FROM message_hidden h
				WHERE h.subject = $1 AND h.chat_id = x.chat_id AND h.message_id = x.message_id
			)
			ORDER BY x.message_id DESC LIMIT 1
		), 0),
		COALESCE(o.subject, ''), COALESCE(p.first_name, ''), COALESCE(p.last_name, ''), COALESCE(p.username, ''),
		CASE WHEN COALESCE(q.photos, 0) = 2 OR b.owner IS NOT NULL OR (q.photos = 1 AND t.owner IS NULL) THEN ''
			ELSE COALESCE(p.photo_media_id, '') END,
		CASE WHEN COALESCE(q.photos, 0) = 2 OR b.owner IS NOT NULL OR (q.photos = 1 AND t.owner IS NULL) THEN ''
			ELSE COALESCE(p.video_media_id, '') END,
		COALESCE(m.is_pinned, false), COALESCE(m.is_muted, false),
		COALESCE(m.read_up_to, 0), COALESCE(o.read_up_to, 0),
		CASE WHEN m.subject IS NULL THEN 0 ELSE (
			SELECT count(*) FROM (
				SELECT 1 FROM messages x
				WHERE x.chat_id = c.id AND x.thread_id = 0 AND x.deleted_at IS NULL AND x.sender <> $1
					AND x.message_id > COALESCE(m.read_up_to, 0) AND NOT EXISTS (
						SELECT 1 FROM message_hidden h
						WHERE h.subject = $1 AND h.chat_id = x.chat_id AND h.message_id = x.message_id
					)
				LIMIT 1000
			) u
		) END,
		COALESCE(m.is_archived, false), COALESCE(d.text, ''),
		COALESCE((extract(epoch FROM d.updated_at) * 1000)::bigint, 0),
		COALESCE(o.delivered_up_to, 0), COALESCE(m.rights, 0), c.member_rights,
		c.no_copy, c.signs, c.reactions_mode, c.reactions, c.reactions_per_post
	FROM chats c
	LEFT JOIN chat_members m ON m.chat_id = c.id AND m.subject = $1
	LEFT JOIN chat_members o ON c.kind = 0 AND o.chat_id = c.id AND o.subject <> $1
	LEFT JOIN profiles p ON p.subject = o.subject
	LEFT JOIN profile_privacy q ON q.subject = o.subject
	LEFT JOIN blocks b ON b.owner = o.subject AND b.blocked = $1
	LEFT JOIN contacts t ON t.owner = o.subject AND t.contact = $1
	LEFT JOIN chat_drafts d ON d.chat_id = c.id AND d.subject = $1`

func (r *Messages) Chats(ctx context.Context, subject string, before int64, limit int) ([]domain.Chat, error) {
	return r.selectChats(ctx, subject, chatSelect+`
		WHERE m.subject IS NOT NULL AND (c.kind <> 0 OR c.last_message_id IS NOT NULL)
			AND ($2 = 0 OR COALESCE(c.last_message_id, 0) < $2)
		ORDER BY COALESCE(c.last_message_id, 0) DESC
		LIMIT $3`, before, limit)
}

func (r *Messages) Chat(ctx context.Context, chatID, subject string) (domain.Chat, error) {
	chats, err := r.selectChats(ctx, subject, chatSelect+`
		WHERE c.id = $2 AND (m.subject IS NOT NULL OR c.link IS NOT NULL)`, chatID)
	if err != nil {
		return domain.Chat{}, err
	}

	if len(chats) == 0 {
		return domain.Chat{}, domain.ErrChatNotFound
	}

	return chats[0], nil
}

func (r *Messages) SearchPublic(ctx context.Context, subject, query string, limit int) ([]domain.Chat, error) {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(query))

	return r.selectChats(ctx, subject, chatSelect+`
		WHERE c.link IS NOT NULL AND (c.link LIKE $2 || '%' OR lower(c.title) LIKE '%' || $2 || '%')
		ORDER BY c.member_count DESC, c.id
		LIMIT $3`, escaped, limit)
}

func (r *Messages) selectChats(ctx context.Context, subject, query string, args ...any) ([]domain.Chat, error) {
	rows, err := r.pool.Query(ctx, query, append([]any{subject}, args...)...)
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
			&chat.ID, &chat.Kind, &chat.Title, &chat.About, &chat.Link, &chat.InviteToken, &chat.MemberCount, &chat.Role,
			&chat.IsMember, &last, &peer.Subject, &peer.FirstName, &peer.LastName, &peer.Username,
			&peer.PhotoMediaID, &peer.VideoMediaID, &chat.IsPinned, &chat.IsMuted,
			&chat.MyReadID, &chat.PeerReadID, &chat.Unread, &chat.IsArchived, &chat.Draft, &chat.DraftAtMs,
			&chat.PeerDeliveredID, &chat.AdminRights, &chat.MemberRights,
			&chat.Rules.NoCopy, &chat.Rules.Signs, &chat.Rules.ReactionsMode, &chat.Rules.Reactions, &chat.Rules.PerPost,
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

	if err := attachMineByRef(ctx, r.pool, subject, found); err != nil {
		return nil, err
	}

	for index, ref := range refs {
		if last, ok := found[ref]; ok {
			visible := last.ForViewer(subject)
			chats[index].Last = &visible
		}
	}

	return chats, nil
}

func (r *Messages) Create(ctx context.Context, next domain.NewChat, people []string) (string, []string, error) {
	id := next.ID
	members := append([]string{next.CreatedBy}, people...)

	var woken []string

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO chats (
				id, kind, title, about, link, invite_token, member_count, created_by, client_id, created_at
			) VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8, $9, $10)
			ON CONFLICT (created_by, client_id) DO NOTHING
			RETURNING id`,
			next.ID, next.Kind, next.Title, next.About, next.Link, next.InviteToken, len(members),
			next.CreatedBy, next.ClientID, next.CreatedAt,
		).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return existingChat(ctx, tx, next, &id)
		}

		if err != nil {
			return chatInsertError(err)
		}

		for index, member := range members {
			role := domain.RoleMember
			if index == 0 {
				role = domain.RoleOwner
			}

			if err := insertMember(ctx, tx, id, member, role, next.CreatedAt); err != nil {
				return err
			}

			if err := appendUpdate(ctx, tx, member, update.KindChats); err != nil {
				return err
			}
		}

		woken = members

		return nil
	})
	if err != nil {
		return "", nil, fmt.Errorf("create chat: %w", err)
	}

	return id, woken, nil
}

func existingChat(ctx context.Context, tx pgx.Tx, next domain.NewChat, id *string) error {
	err := tx.QueryRow(ctx, `SELECT id FROM chats WHERE created_by = $1 AND client_id = $2`,
		next.CreatedBy, next.ClientID,
	).Scan(id)
	if err != nil {
		return fmt.Errorf("select repeated chat: %w", err)
	}

	return nil
}

func chatInsertError(err error) error {
	var failure *pgconn.PgError
	if errors.As(err, &failure) && failure.Code == uniqueViolation && failure.ConstraintName == linkConstraint {
		return domain.ErrLinkTaken
	}

	return fmt.Errorf("insert chat: %w", err)
}

func insertMember(ctx context.Context, tx pgx.Tx, chatID, subject string, role domain.Role, now time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO chat_members (chat_id, subject, role, joined_at) VALUES ($1, $2, $3, $4)`,
		chatID, subject, role, now,
	)
	if err != nil {
		return fmt.Errorf("insert member: %w", err)
	}

	return nil
}

func lockChat(ctx context.Context, tx pgx.Tx, chatID string) (domain.Kind, error) {
	var kind domain.Kind

	err := tx.QueryRow(ctx, `SELECT kind FROM chats WHERE id = $1 FOR UPDATE`, chatID).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrChatNotFound
	}

	if err != nil {
		return 0, fmt.Errorf("lock chat: %w", err)
	}

	if kind == domain.KindDirect {
		return 0, domain.ErrNotAllowed
	}

	return kind, nil
}

func roleIn(ctx context.Context, tx pgx.Tx, chatID, subject string) (domain.Role, bool, error) {
	var role domain.Role

	err := tx.QueryRow(ctx, `SELECT role FROM chat_members WHERE chat_id = $1 AND subject = $2`, chatID, subject).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}

	if err != nil {
		return 0, false, fmt.Errorf("select role: %w", err)
	}

	return role, true, nil
}

func (r *Messages) IsMember(ctx context.Context, chatID, subject string) (bool, error) {
	var found bool

	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM chat_members WHERE chat_id = $1 AND subject = $2)`, chatID, subject,
	).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("check member: %w", err)
	}

	return found, nil
}

func (r *Messages) AddMember(
	ctx context.Context,
	chatID, actor, subject string,
	role domain.Role,
	rights domain.Rights,
	now time.Time,
) ([]string, error) {
	var woken []string

	if role == domain.RoleMember {
		rights = 0
	}

	rights = domain.CleanAdminRights(rights)

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		kind, err := lockChat(ctx, tx, chatID)
		if err != nil {
			return err
		}

		access, err := requireAccess(ctx, tx, chatID, actor)
		if err != nil {
			return err
		}

		current, exists, err := accessIn(ctx, tx, chatID, subject)
		if err != nil {
			return err
		}

		changed, err := applyMember(ctx, tx, memberChange{
			chatID: chatID, actor: access, by: actor, subject: subject, role: role, rights: rights, current: current,
			exists: exists, now: now,
		})
		if err != nil || !changed {
			return err
		}

		woken, err = wakeAfterChange(ctx, tx, chatID, kind, subject)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("add member: %w", err)
	}

	return woken, nil
}

type memberChange struct {
	chatID  string
	actor   domain.Access
	by      string
	subject string
	role    domain.Role
	rights  domain.Rights
	current domain.Access
	exists  bool
	now     time.Time
}

func applyMember(ctx context.Context, tx pgx.Tx, change memberChange) (bool, error) {
	if !change.exists {
		if err := checkMayAdd(ctx, tx, change); err != nil {
			return false, err
		}

		if err := addMember(ctx, tx, change.chatID, change.subject, change.role, change.now); err != nil {
			return false, err
		}

		if err := setAdminRights(ctx, tx, change); err != nil {
			return false, err
		}

		return true, logRole(ctx, tx, change, domain.RoleMember)
	}

	if change.current.Role == change.role && change.current.Admin == change.rights {
		return false, nil
	}

	if !change.actor.Can(domain.RightPromote) || !change.actor.Role.Manages(change.current.Role) ||
		!change.actor.CanGrant(change.rights) {
		return false, domain.ErrNotAllowed
	}

	_, err := tx.Exec(ctx, `
		UPDATE chat_members SET role = $3, rights = $4 WHERE chat_id = $1 AND subject = $2`,
		change.chatID, change.subject, change.role, change.rights,
	)
	if err != nil {
		return false, fmt.Errorf("update role: %w", err)
	}

	return true, logRole(ctx, tx, change, change.current.Role)
}

func logRole(ctx context.Context, tx pgx.Tx, change memberChange, before domain.Role) error {
	var kind domain.LogKind

	switch {
	case before == domain.RoleMember && change.role == domain.RoleAdmin:
		kind = domain.LogAdminAdded
	case before == domain.RoleAdmin && change.role == domain.RoleMember:
		kind = domain.LogAdminRemoved
	default:
		return nil
	}

	return insertLog(ctx, tx, change.chatID, change.by, change.subject, domain.LogEntry{Kind: kind})
}

func checkMayAdd(ctx context.Context, tx pgx.Tx, change memberChange) error {
	if change.role == domain.RoleAdmin {
		if !change.actor.Can(domain.RightPromote) || !change.actor.CanGrant(change.rights) {
			return domain.ErrNotAllowed
		}
	} else if !change.actor.Can(domain.RightInvite) {
		return domain.ErrNotAllowed
	}

	banned, err := isBanned(ctx, tx, change.chatID, change.subject)
	if err != nil {
		return err
	}

	if banned {
		return domain.ErrNotAllowed
	}

	return nil
}

func setAdminRights(ctx context.Context, tx pgx.Tx, change memberChange) error {
	if change.role != domain.RoleAdmin {
		return nil
	}

	_, err := tx.Exec(ctx, `
		UPDATE chat_members SET rights = $3 WHERE chat_id = $1 AND subject = $2`,
		change.chatID, change.subject, change.rights,
	)
	if err != nil {
		return fmt.Errorf("set rights: %w", err)
	}

	return nil
}

func addMember(ctx context.Context, tx pgx.Tx, chatID, subject string, role domain.Role, now time.Time) error {
	if err := insertMember(ctx, tx, chatID, subject, role, now); err != nil {
		return err
	}

	_, err := tx.Exec(ctx, `UPDATE chats SET member_count = member_count + 1 WHERE id = $1`, chatID)
	if err != nil {
		return fmt.Errorf("count member: %w", err)
	}

	return enqueueChatEvent(ctx, tx, agent.TriggerJoined, chatID, subject, "", now)
}

func wakeAfterChange(ctx context.Context, tx pgx.Tx, chatID string, kind domain.Kind, subject string) ([]string, error) {
	woken := []string{subject}

	if kind == domain.KindGroup {
		members, err := membersOf(ctx, tx, chatID)
		if err != nil {
			return nil, err
		}

		for _, member := range members {
			if member != subject {
				woken = append(woken, member)
			}
		}
	}

	for _, member := range woken {
		if err := appendUpdate(ctx, tx, member, update.KindChats); err != nil {
			return nil, err
		}
	}

	return woken, nil
}

func (r *Messages) Join(ctx context.Context, code, subject string, now time.Time) (string, []string, error) {
	var (
		chatID string
		woken  []string
	)

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var kind domain.Kind

		err := tx.QueryRow(ctx, `
			SELECT id, kind FROM chats WHERE link = lower($1) OR invite_token = $1 LIMIT 1 FOR UPDATE`, code,
		).Scan(&chatID, &kind)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrChatNotFound
		}

		if err != nil {
			return fmt.Errorf("select chat by code: %w", err)
		}

		_, exists, err := roleIn(ctx, tx, chatID, subject)
		if err != nil || exists {
			return err
		}

		banned, err := isBanned(ctx, tx, chatID, subject)
		if err != nil {
			return err
		}

		if banned {
			return domain.ErrNotAllowed
		}

		if err := addMember(ctx, tx, chatID, subject, domain.RoleMember, now); err != nil {
			return err
		}

		woken, err = wakeAfterChange(ctx, tx, chatID, kind, subject)

		return err
	})
	if err != nil {
		return "", nil, fmt.Errorf("join chat: %w", err)
	}

	return chatID, woken, nil
}

func (r *Messages) RemoveMember(
	ctx context.Context,
	chatID, actor, subject string,
	ban bool,
	now time.Time,
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

		target, exists, err := accessIn(ctx, tx, chatID, subject)
		if err != nil || !exists {
			return err
		}

		if actor != subject && (!access.Can(domain.RightBan) || !access.Role.Manages(target.Role)) {
			return domain.ErrNotAllowed
		}

		if err := dropMember(ctx, tx, chatID, subject, target.Role); err != nil {
			return err
		}

		if ban && actor != subject {
			if err := insertBan(ctx, tx, chatID, subject, actor, now); err != nil {
				return err
			}

			if err := insertLog(ctx, tx, chatID, actor, subject, domain.LogEntry{Kind: domain.LogBanned}); err != nil {
				return err
			}
		}

		woken, err = wakeAfterChange(ctx, tx, chatID, kind, subject)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("remove member: %w", err)
	}

	return woken, nil
}

func dropMember(ctx context.Context, tx pgx.Tx, chatID, subject string, role domain.Role) error {
	_, err := tx.Exec(ctx, `DELETE FROM chat_members WHERE chat_id = $1 AND subject = $2`, chatID, subject)
	if err != nil {
		return fmt.Errorf("delete member: %w", err)
	}

	_, err = tx.Exec(ctx, `UPDATE chats SET member_count = member_count - 1 WHERE id = $1`, chatID)
	if err != nil {
		return fmt.Errorf("count member: %w", err)
	}

	if role != domain.RoleOwner {
		return nil
	}

	_, err = tx.Exec(ctx, `
		UPDATE chat_members SET role = $2
		WHERE chat_id = $1 AND subject = (
			SELECT subject FROM chat_members WHERE chat_id = $1 ORDER BY role DESC, joined_at, subject LIMIT 1
		)`, chatID, domain.RoleOwner,
	)
	if err != nil {
		return fmt.Errorf("pass ownership: %w", err)
	}

	return nil
}

func (r *Messages) Members(ctx context.Context, chatID, subject string, limit int) ([]domain.Member, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT o.subject, COALESCE(p.first_name, ''), COALESCE(p.last_name, ''), COALESCE(p.username, ''),
			CASE WHEN COALESCE(q.photos, 0) = 0 THEN COALESCE(p.photo_media_id, '') ELSE '' END,
			CASE WHEN COALESCE(q.photos, 0) = 0 THEN COALESCE(p.video_media_id, '') ELSE '' END,
			o.role, o.rights
		FROM chat_members m
		JOIN chats c ON c.id = m.chat_id
		JOIN chat_members o ON o.chat_id = m.chat_id
		LEFT JOIN profiles p ON p.subject = o.subject
		LEFT JOIN profile_privacy q ON q.subject = o.subject
		WHERE m.chat_id = $1 AND m.subject = $2 AND (c.kind <> $4 OR m.role >= $5 OR o.role >= $5)
		ORDER BY o.role DESC, o.joined_at, o.subject
		LIMIT $3`, chatID, subject, limit, domain.KindChannel, domain.RoleAdmin,
	)
	if err != nil {
		return nil, fmt.Errorf("select members: %w", err)
	}

	members, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Member, error) {
		var next domain.Member

		peer := &next.Peer
		err := row.Scan(
			&peer.Subject, &peer.FirstName, &peer.LastName, &peer.Username, &peer.PhotoMediaID, &peer.VideoMediaID, &next.Role, &next.Rights,
		)

		return next, err
	})
	if err != nil {
		return nil, fmt.Errorf("read members: %w", err)
	}

	if len(members) == 0 {
		return nil, domain.ErrChatNotFound
	}

	return members, nil
}

func (r *Messages) SetPinned(ctx context.Context, chatID, subject string, isPinned bool) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var current bool

		err := tx.QueryRow(ctx, `
			SELECT is_pinned FROM chat_members WHERE chat_id = $1 AND subject = $2 FOR UPDATE`, chatID, subject,
		).Scan(&current)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrChatNotFound
		}

		if err != nil {
			return fmt.Errorf("select pin: %w", err)
		}

		if current == isPinned {
			return nil
		}

		if err := checkPinLimit(ctx, tx, subject, isPinned); err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			UPDATE chat_members SET is_pinned = $3 WHERE chat_id = $1 AND subject = $2`, chatID, subject, isPinned,
		)
		if err != nil {
			return fmt.Errorf("update pin: %w", err)
		}

		return appendUpdate(ctx, tx, subject, update.KindChats)
	})
	if err != nil {
		return fmt.Errorf("set pin: %w", err)
	}

	return nil
}

func (r *Messages) SetMuted(ctx context.Context, chatID, subject string, isMuted bool) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE chat_members SET is_muted = $3 WHERE chat_id = $1 AND subject = $2`, chatID, subject, isMuted,
		)
		if err != nil {
			return fmt.Errorf("update mute: %w", err)
		}

		if tag.RowsAffected() == 0 {
			return domain.ErrChatNotFound
		}

		return appendUpdate(ctx, tx, subject, update.KindChats)
	})
	if err != nil {
		return fmt.Errorf("set mute: %w", err)
	}

	return nil
}

func checkPinLimit(ctx context.Context, tx pgx.Tx, subject string, isPinned bool) error {
	if !isPinned {
		return nil
	}

	var count int

	err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_members WHERE subject = $1 AND is_pinned`, subject).Scan(&count)
	if err != nil {
		return fmt.Errorf("count pins: %w", err)
	}

	if count >= domain.MaxPinnedChats {
		return domain.ErrTooManyPinned
	}

	return nil
}

func (r *Messages) ChatByCode(ctx context.Context, code, subject string) (domain.Chat, error) {
	if code == "" {
		return domain.Chat{}, domain.ErrChatNotFound
	}

	chats, err := r.selectChats(ctx, subject, chatSelect+`
		WHERE c.link = lower($2) OR c.invite_token = $2
		LIMIT 1`, code)
	if err != nil {
		return domain.Chat{}, err
	}

	if len(chats) == 0 {
		return domain.Chat{}, domain.ErrChatNotFound
	}

	chat := chats[0]
	if chat.Link == "" && !chat.IsMember {
		chat.Last = nil
	}

	return chat, nil
}

func (r *Messages) ResetInvite(ctx context.Context, chatID, actor, token string) error {
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := lockChat(ctx, tx, chatID); err != nil {
			return err
		}

		access, err := requireAccess(ctx, tx, chatID, actor)
		if err != nil {
			return err
		}

		if access.Role < domain.RoleAdmin || !access.Can(domain.RightInvite) {
			return domain.ErrNotAllowed
		}

		tag, err := tx.Exec(ctx, `UPDATE chats SET invite_token = $2 WHERE id = $1 AND link IS NULL`, chatID, token)
		if err != nil {
			return fmt.Errorf("update invite: %w", err)
		}

		if tag.RowsAffected() == 0 {
			return domain.ErrNotAllowed
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("reset invite: %w", err)
	}

	return nil
}

func (r *Messages) PersonByUsername(ctx context.Context, username, viewer string) (directory.Person, error) {
	var person directory.Person

	err := r.pool.QueryRow(ctx, `
		SELECT p.subject, p.first_name, p.last_name, p.username,
			CASE WHEN COALESCE(q.photos, 0) = 0 THEN p.photo_media_id ELSE '' END,
			CASE WHEN COALESCE(q.photos, 0) = 0 THEN p.video_media_id ELSE '' END
		FROM profiles p
		LEFT JOIN profile_privacy q ON q.subject = p.subject
		WHERE lower(p.username) = lower($1) AND COALESCE(q.search, 0) <> 2
			AND NOT EXISTS (
				SELECT 1 FROM blocks b
				WHERE (b.owner = p.subject AND b.blocked = $2) OR (b.owner = $2 AND b.blocked = p.subject)
			)`, username, viewer,
	).Scan(
		&person.Subject, &person.FirstName, &person.LastName, &person.Username,
		&person.PhotoMediaID, &person.VideoMediaID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return directory.Person{}, directory.ErrNotFound
	}

	if err != nil {
		return directory.Person{}, fmt.Errorf("select username: %w", err)
	}

	return person, nil
}
