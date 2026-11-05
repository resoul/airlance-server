package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "go.airlance.org/server/internal/domain/folder"
	"go.airlance.org/server/internal/domain/update"
)

type folderRow struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Emoji      string   `json:"emoji,omitempty"`
	Color      int32    `json:"color"`
	Categories int32    `json:"categories"`
	Exclusions int32    `json:"exclusions"`
	Included   []string `json:"included,omitempty"`
	Excluded   []string `json:"excluded,omitempty"`
}

type Folders struct {
	pool *pgxpool.Pool
}

func NewFolders(pool *pgxpool.Pool) *Folders {
	return &Folders{pool: pool}
}

func (r *Folders) Get(ctx context.Context, owner string) (domain.List, error) {
	var (
		raw  []byte
		list domain.List
	)

	err := r.pool.QueryRow(ctx, `
		SELECT folders, all_chats_position, shows_tags, revision FROM chat_folder_lists WHERE owner = $1`, owner,
	).Scan(&raw, &list.AllChatsPosition, &list.ShowsTags, &list.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Empty(), nil
	}

	if err != nil {
		return domain.List{}, fmt.Errorf("select folders: %w", err)
	}

	var rows []folderRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		return domain.List{}, fmt.Errorf("decode folders: %w", err)
	}

	list.Folders = make([]domain.Folder, len(rows))
	for index, next := range rows {
		list.Folders[index] = domain.Folder(next)
	}

	return list, nil
}

func (r *Folders) Put(ctx context.Context, owner string, list domain.List, now time.Time) (domain.List, error) {
	rows := make([]folderRow, len(list.Folders))
	for index, next := range list.Folders {
		rows[index] = folderRow(next)
	}

	raw, err := json.Marshal(rows)
	if err != nil {
		return domain.List{}, fmt.Errorf("encode folders: %w", err)
	}

	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO chat_folder_lists (owner, folders, all_chats_position, shows_tags, revision, updated_at)
			VALUES ($1, $2, $3, $4, 1, $5)
			ON CONFLICT (owner) DO UPDATE SET
				folders = excluded.folders,
				all_chats_position = excluded.all_chats_position,
				shows_tags = excluded.shows_tags,
				revision = chat_folder_lists.revision + 1,
				updated_at = excluded.updated_at
			RETURNING revision`, owner, raw, list.AllChatsPosition, list.ShowsTags, now,
		).Scan(&list.Revision)
		if err != nil {
			return fmt.Errorf("save folders: %w", err)
		}

		return appendUpdate(ctx, tx, owner, update.KindFolders)
	})
	if err != nil {
		return domain.List{}, fmt.Errorf("put folders: %w", err)
	}

	return list, nil
}
