package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	usecase "go.airlance.org/server/internal/usecase/directory"
)

type SearchQueue struct {
	pool *pgxpool.Pool
}

func NewSearchQueue(pool *pgxpool.Pool) *SearchQueue {
	return &SearchQueue{pool: pool}
}

func (r *SearchQueue) Pending(ctx context.Context, limit int) ([]usecase.QueuedPerson, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT q.version, p.subject, p.first_name, p.last_name, p.username
		FROM search_queue q
		JOIN profiles p ON p.subject = q.subject
		ORDER BY q.version
		LIMIT $1`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("select queue: %w", err)
	}
	defer rows.Close()

	var pending []usecase.QueuedPerson

	for rows.Next() {
		var (
			next     usecase.QueuedPerson
			username *string
		)

		person := &next.Person
		if err := rows.Scan(&next.Version, &person.Subject, &person.FirstName, &person.LastName, &username); err != nil {
			return nil, fmt.Errorf("scan queue: %w", err)
		}

		if username != nil {
			person.Username = *username
		}

		pending = append(pending, next)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read queue: %w", err)
	}

	return pending, nil
}

func (r *SearchQueue) Done(ctx context.Context, indexed []usecase.QueuedPerson) error {
	subjects := make([]string, len(indexed))
	versions := make([]int64, len(indexed))

	for index, queued := range indexed {
		subjects[index] = queued.Person.Subject
		versions[index] = queued.Version
	}

	_, err := r.pool.Exec(ctx, `
		DELETE FROM search_queue q
		USING unnest($1::text[], $2::bigint[]) AS done (subject, version)
		WHERE q.subject = done.subject AND q.version = done.version`, subjects, versions,
	)
	if err != nil {
		return fmt.Errorf("delete queue: %w", err)
	}

	return nil
}

func enqueueForSearch(ctx context.Context, tx pgx.Tx, subject string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO search_queue (subject) VALUES ($1)
		ON CONFLICT (subject) DO UPDATE SET version = nextval('search_queue_version')`, subject,
	)
	if err != nil {
		return fmt.Errorf("enqueue for search: %w", err)
	}

	return nil
}
