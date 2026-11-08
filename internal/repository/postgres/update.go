package postgres

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/profile"
	"go.airlance.org/server/internal/domain/update"
)

type Updates struct {
	pool *pgxpool.Pool
}

func NewUpdates(pool *pgxpool.Pool) *Updates {
	return &Updates{pool: pool}
}

func (r *Updates) Latest(ctx context.Context, subject string) (uint64, error) {
	var seq int64

	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE((SELECT seq FROM update_heads WHERE subject = $1), 0)`, subject,
	).Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("select latest update: %w", err)
	}

	return uint64(seq), nil
}

func (r *Updates) After(ctx context.Context, subject string, after uint64, limit int) ([]update.Update, error) {
	updates, refs, needsProfile, err := r.journal(ctx, subject, after, limit)
	if err != nil {
		return nil, err
	}

	var own profile.Profile
	if needsProfile {
		own, err = NewProfiles(r.pool).Get(ctx, subject)
		if err != nil {
			return nil, fmt.Errorf("select profile of an update: %w", err)
		}
	}

	found, err := messagesByRef(ctx, r.pool, refs)
	if err != nil {
		return nil, err
	}

	hidden, err := hiddenFor(ctx, r.pool, subject, refs)
	if err != nil {
		return nil, err
	}

	if err := attachMineByRef(ctx, r.pool, subject, found); err != nil {
		return nil, err
	}

	pins, err := pinsFor(ctx, r.pool, pinChats(updates))
	if err != nil {
		return nil, err
	}

	for index := range updates {
		next := &updates[index]
		if next.Kind == update.KindSettings {
			next.Setting, err = readSettings(ctx, r.pool, subject, next.Message.ChatID)
			if err != nil {
				return nil, err
			}

			continue
		}

		if next.Kind == update.KindDraft {
			next.Draft, err = draftState(ctx, r.pool, subject, next.Message.ChatID)
			if err != nil {
				return nil, err
			}

			continue
		}

		if next.Kind == update.KindRead {
			next.Read, err = readState(ctx, r.pool, subject, next.Message.ChatID)
			if err != nil {
				return nil, err
			}

			continue
		}

		if next.Kind == update.KindPins {
			next.Pins = pins[next.Message.ChatID]

			continue
		}

		if next.Kind == update.KindProfile {
			next.Profile = own

			continue
		}

		if next.Kind != update.KindMessage {
			continue
		}

		message, ok := found[messageRef{chatID: next.Message.ChatID, id: next.Message.ID}]
		if !ok {
			return nil, fmt.Errorf("update %d points to a missing message", next.Seq)
		}

		if message.IsDeleted || hidden[messageRef{chatID: message.ChatID, id: message.ID}] {
			next.Kind = update.KindGone
			next.Message = domain.Message{ChatID: message.ChatID, ID: message.ID}

			continue
		}

		next.Message = message.ForViewer(subject)
	}

	return updates, nil
}

func (r *Updates) journal(
	ctx context.Context,
	subject string,
	after uint64,
	limit int,
) ([]update.Update, []messageRef, bool, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT seq, kind, COALESCE(chat_id, ''), COALESCE(message_id, 0)
		FROM updates
		WHERE subject = $1 AND seq > $2
		ORDER BY seq
		LIMIT $3`, subject, int64(after), limit,
	)
	if err != nil {
		return nil, nil, false, fmt.Errorf("select updates: %w", err)
	}
	defer rows.Close()

	var (
		updates      []update.Update
		refs         []messageRef
		needsProfile bool
	)

	for rows.Next() {
		var (
			next update.Update
			seq  int64
		)

		if err := rows.Scan(&seq, &next.Kind, &next.Message.ChatID, &next.Message.ID); err != nil {
			return nil, nil, false, fmt.Errorf("scan update: %w", err)
		}

		next.Seq = uint64(seq)

		switch next.Kind {
		case update.KindProfile:
			needsProfile = true
		case update.KindMessage:
			refs = append(refs, messageRef{chatID: next.Message.ChatID, id: next.Message.ID})
		case update.KindStories, update.KindContacts, update.KindChats, update.KindScheduled, update.KindFolders,
			update.KindGone, update.KindPins, update.KindRead, update.KindDraft, update.KindSettings:
		}

		updates = append(updates, next)
	}

	if err := rows.Err(); err != nil {
		return nil, nil, false, fmt.Errorf("read updates: %w", err)
	}

	return updates, refs, needsProfile, nil
}

func appendUpdate(ctx context.Context, tx pgx.Tx, subject string, kind update.Kind) error {
	_, err := tx.Exec(ctx, `
		WITH head AS (
			INSERT INTO update_heads (subject, seq) VALUES ($1, 1)
			ON CONFLICT (subject) DO UPDATE SET seq = update_heads.seq + 1
			RETURNING seq
		)
		INSERT INTO updates (subject, seq, kind) SELECT $1, seq, $2 FROM head`, subject, int16(kind),
	)
	if err != nil {
		return fmt.Errorf("append update: %w", err)
	}

	return nil
}

func appendMessageUpdate(ctx context.Context, tx pgx.Tx, subject, chatID string, messageID int64) error {
	_, err := tx.Exec(ctx, `
		WITH head AS (
			INSERT INTO update_heads (subject, seq) VALUES ($1, 1)
			ON CONFLICT (subject) DO UPDATE SET seq = update_heads.seq + 1
			RETURNING seq
		)
		INSERT INTO updates (subject, seq, kind, chat_id, message_id) SELECT $1, seq, $2, $3, $4 FROM head`,
		subject, int16(update.KindMessage), chatID, messageID,
	)
	if err != nil {
		return fmt.Errorf("append message update: %w", err)
	}

	return nil
}

func pinChats(updates []update.Update) []string {
	var chats []string

	for _, next := range updates {
		if next.Kind == update.KindPins && !slices.Contains(chats, next.Message.ChatID) {
			chats = append(chats, next.Message.ChatID)
		}
	}

	return chats
}
