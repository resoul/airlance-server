package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	domain "go.airlance.org/server/internal/domain/message"
)

func insertLog(ctx context.Context, tx pgx.Tx, chatID, actor, target string, entry domain.LogEntry) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO chat_log (chat_id, actor, kind, target, detail) VALUES ($1, $2, $3, $4, $5)`,
		chatID, actor, int16(entry.Kind), target, entry.Detail,
	)
	if err != nil {
		return fmt.Errorf("insert log: %w", err)
	}

	return nil
}

func insertLogs(ctx context.Context, tx pgx.Tx, chatID, actor string, entries []domain.LogEntry) error {
	for _, entry := range entries {
		if err := insertLog(ctx, tx, chatID, actor, "", entry); err != nil {
			return err
		}
	}

	return nil
}

func kindEntries(kinds []domain.LogKind) []domain.LogEntry {
	entries := make([]domain.LogEntry, len(kinds))

	for index, kind := range kinds {
		entries[index] = domain.LogEntry{Kind: kind}
	}

	return entries
}

func (r *Messages) ChatLog(ctx context.Context, chatID, actor string, limit int) ([]domain.LogEntry, error) {
	var found []domain.LogEntry

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		access, err := requireAccess(ctx, tx, chatID, actor)
		if err != nil {
			return err
		}

		if access.Kind == domain.KindDirect || access.Role < domain.RoleAdmin {
			return domain.ErrNotAllowed
		}

		rows, err := tx.Query(ctx, `
			SELECT l.id, l.actor, COALESCE(p.first_name, ''), COALESCE(p.last_name, ''), COALESCE(p.username, ''),
				l.kind, CASE WHEN l.target = '' THEN l.detail ELSE COALESCE(NULLIF(btrim(t.first_name || ' ' || t.last_name), ''),
				t.username, '') END, l.created_at
			FROM chat_log l
			LEFT JOIN profiles p ON p.subject = l.actor
			LEFT JOIN profiles t ON t.subject = l.target
			WHERE l.chat_id = $1 ORDER BY l.id DESC LIMIT $2`, chatID, limit,
		)
		if err != nil {
			return fmt.Errorf("select log: %w", err)
		}

		found, err = pgx.CollectRows(rows, scanLog)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("read log: %w", err)
	}

	return found, nil
}

func scanLog(row pgx.CollectableRow) (domain.LogEntry, error) {
	var (
		next domain.LogEntry
		kind int16
		at   time.Time
	)

	err := row.Scan(&next.ID, &next.Actor.Subject, &next.Actor.FirstName, &next.Actor.LastName,
		&next.Actor.Username, &kind, &next.Detail, &at)
	next.Kind = domain.LogKind(kind)
	next.At = at

	return next, err
}
