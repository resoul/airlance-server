package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "go.airlance.org/server/internal/domain/media"
)

type Media struct {
	pool *pgxpool.Pool
}

func NewMedia(pool *pgxpool.Pool) *Media {
	return &Media{pool: pool}
}

func (r *Media) Create(ctx context.Context, object domain.Object) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO media_objects (id, owner, size, created_at) VALUES ($1, $2, $3, $4)`,
		object.ID, object.Owner, object.Size, object.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert media: %w", err)
	}

	return nil
}

func (r *Media) Get(ctx context.Context, id string) (domain.Object, error) {
	got := domain.Object{ID: id}

	err := r.pool.QueryRow(ctx, `
		SELECT owner, size, created_at FROM media_objects WHERE id = $1`, id,
	).Scan(&got.Owner, &got.Size, &got.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Object{}, domain.ErrNotFound
	}

	if err != nil {
		return domain.Object{}, fmt.Errorf("select media: %w", err)
	}

	return got, nil
}
