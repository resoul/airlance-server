package directory

import (
	"context"
	"fmt"
	"time"

	domain "go.airlance.org/server/internal/domain/directory"
)

const (
	indexBatch = 500
	retryEvery = 30 * time.Second
)

type QueuedPerson struct {
	Person  domain.Person
	Version int64
}

type Queue interface {
	Pending(ctx context.Context, limit int) ([]QueuedPerson, error)
	Done(ctx context.Context, indexed []QueuedPerson) error
}

type Indexer struct {
	queue      Queue
	index      Index
	onError    func(error)
	wake       chan struct{}
	retry      time.Duration
	configured bool
}

func NewIndexer(queue Queue, index Index, onError func(error)) *Indexer {
	return &Indexer{queue: queue, index: index, onError: onError, wake: make(chan struct{}, 1), retry: retryEvery}
}

func (i *Indexer) Notify(string) {
	select {
	case i.wake <- struct{}{}:
	default:
	}
}

func (i *Indexer) SetRetry(every time.Duration) {
	i.retry = every
}

func (i *Indexer) Run(ctx context.Context) {
	ticker := time.NewTicker(i.retry)
	defer ticker.Stop()

	for {
		if err := i.drain(ctx); err != nil && ctx.Err() == nil {
			i.onError(err)
		}

		select {
		case <-ctx.Done():
			return
		case <-i.wake:
		case <-ticker.C:
		}
	}
}

func (i *Indexer) drain(ctx context.Context) error {
	if !i.configured {
		if err := i.index.Configure(ctx); err != nil {
			return fmt.Errorf("configure index: %w", err)
		}

		i.configured = true
	}

	for {
		pending, err := i.queue.Pending(ctx, indexBatch)
		if err != nil {
			return fmt.Errorf("read queue: %w", err)
		}

		if len(pending) == 0 {
			return nil
		}

		people := make([]domain.Person, len(pending))
		for index, queued := range pending {
			people[index] = queued.Person
		}

		if err := i.index.Upsert(ctx, people); err != nil {
			return fmt.Errorf("index people: %w", err)
		}

		if err := i.queue.Done(ctx, pending); err != nil {
			return fmt.Errorf("clear queue: %w", err)
		}
	}
}
