package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	wakeChannel    = "airlance_updates"
	wakeRetryEvery = 5 * time.Second
	wakeTimeout    = 5 * time.Second
)

type Wakes struct {
	pool    *pgxpool.Pool
	onError func(error)
}

func NewWakes(pool *pgxpool.Pool, onError func(error)) *Wakes {
	return &Wakes{pool: pool, onError: onError}
}

func (w *Wakes) Notify(subject string) {
	ctx, cancel := context.WithTimeout(context.Background(), wakeTimeout)
	defer cancel()

	if _, err := w.pool.Exec(ctx, `SELECT pg_notify($1, $2)`, wakeChannel, subject); err != nil {
		w.onError(fmt.Errorf("notify: %w", err))
	}
}

func (w *Wakes) Run(ctx context.Context, onWake func(subject string)) {
	for ctx.Err() == nil {
		err := w.listen(ctx, onWake)
		if ctx.Err() != nil {
			return
		}

		w.onError(err)

		select {
		case <-ctx.Done():
		case <-time.After(wakeRetryEvery):
		}
	}
}

func (w *Wakes) listen(ctx context.Context, onWake func(subject string)) error {
	conn, err := w.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN "+wakeChannel); err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	for {
		next, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return fmt.Errorf("wait: %w", err)
		}

		onWake(next.Payload)
	}
}
