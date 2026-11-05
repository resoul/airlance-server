package directory_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	domain "go.airlance.org/server/internal/domain/directory"
	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/profile"
	"go.airlance.org/server/internal/usecase/directory"
)

type noBlockers struct{}

func (noBlockers) BlockersOf(context.Context, string) ([]string, error) {
	return nil, nil
}

type fakeIndex struct {
	mu         sync.Mutex
	found      []domain.Person
	failSearch bool
	failUpsert bool
	configured int
	upserted   []domain.Person
	seenQuery  string
}

func (f *fakeIndex) Configure(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.configured++

	return nil
}

func (f *fakeIndex) Upsert(_ context.Context, people []domain.Person) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failUpsert {
		return errors.New("index is down")
	}

	f.upserted = append(f.upserted, people...)

	return nil
}

func (f *fakeIndex) Search(_ context.Context, query string, _ int) ([]domain.Person, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.seenQuery = query
	if f.failSearch {
		return nil, errors.New("secret index detail")
	}

	return f.found, nil
}

func (f *fakeIndex) setFailUpsert(fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.failUpsert = fail
}

func (f *fakeIndex) upsertedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.upserted)
}

var reader = identity.Principal{Subject: "anna", Scopes: []string{profile.ScopeRead}}

func newService(index *fakeIndex) *directory.Service {
	now := time.Unix(100, 0)

	return directory.New(index, noBlockers{}, func() time.Time {
		return now
	})
}

func TestSearchNeedsTheRightToRead(t *testing.T) {
	t.Parallel()

	writer := identity.Principal{Subject: "anna", Scopes: []string{profile.ScopeWrite}}
	if _, err := newService(&fakeIndex{}).Search(context.Background(), writer, "petro"); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("got %v", err)
	}
}

func TestSearchCleansTheQueryAndHidesThePersonAsking(t *testing.T) {
	t.Parallel()

	index := &fakeIndex{found: []domain.Person{{Subject: "anna"}, {Subject: "petro"}}}

	people, err := newService(index).Search(context.Background(), reader, " @pet ")
	if err != nil {
		t.Fatal(err)
	}

	if index.seenQuery != "pet" || len(people) != 1 || people[0].Subject != "petro" {
		t.Fatalf("query %q, got %+v", index.seenQuery, people)
	}
}

func TestSearchNeverReturnsMoreThanTheLimit(t *testing.T) {
	t.Parallel()

	index := &fakeIndex{}
	for i := range domain.MaxResults + 1 {
		index.found = append(index.found, domain.Person{Subject: fmt.Sprint("p", i)})
	}

	people, err := newService(index).Search(context.Background(), reader, "abc")
	if err != nil || len(people) != domain.MaxResults {
		t.Fatalf("got %d, %v", len(people), err)
	}
}

func TestAnIndexFailureSaysOnlyThatSearchIsUnavailable(t *testing.T) {
	t.Parallel()

	_, err := newService(&fakeIndex{failSearch: true}).Search(context.Background(), reader, "abc")
	if !errors.Is(err, domain.ErrSearchUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestSearchesAreLimitedPerPersonAndThenAllowedAgain(t *testing.T) {
	t.Parallel()

	now := time.Unix(100, 0)
	service := directory.New(&fakeIndex{}, noBlockers{}, func() time.Time {
		return now
	})
	ctx := context.Background()

	for i := range 60 {
		if _, err := service.Search(ctx, reader, "abc"); err != nil {
			t.Fatalf("search %d: %v", i, err)
		}
	}

	if _, err := service.Search(ctx, reader, "abc"); !errors.Is(err, domain.ErrTooManySearches) {
		t.Fatalf("over the limit: %v", err)
	}

	petro := identity.Principal{Subject: "petro", Scopes: reader.Scopes}
	if _, err := service.Search(ctx, petro, "abc"); err != nil {
		t.Fatalf("another person: %v", err)
	}

	if _, err := service.Search(ctx, reader, "ab"); !errors.Is(err, domain.ErrInvalidQuery) {
		t.Fatalf("a short query is refused first: %v", err)
	}

	now = now.Add(time.Minute)
	if _, err := service.Search(ctx, reader, "abc"); err != nil {
		t.Fatalf("the next minute: %v", err)
	}
}

type fakeQueue struct {
	mu      sync.Mutex
	pending []directory.QueuedPerson
}

func (q *fakeQueue) Pending(_ context.Context, limit int) ([]directory.QueuedPerson, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.pending) < limit {
		limit = len(q.pending)
	}

	return append([]directory.QueuedPerson(nil), q.pending[:limit]...), nil
}

func (q *fakeQueue) Done(_ context.Context, indexed []directory.QueuedPerson) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.pending = q.pending[len(indexed):]

	return nil
}

func (q *fakeQueue) add(subject string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.pending = append(q.pending, directory.QueuedPerson{Person: domain.Person{Subject: subject}})
}

func (q *fakeQueue) size() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	return len(q.pending)
}

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for ", what)
		}

		time.Sleep(5 * time.Millisecond)
	}
}

func TestTheIndexerEmptiesTheQueueAndWakesOnANotification(t *testing.T) {
	t.Parallel()

	queue := &fakeQueue{}
	index := &fakeIndex{}
	for i := range 1200 {
		queue.add(fmt.Sprint("p", i))
	}

	indexer := directory.NewIndexer(queue, index, func(err error) {
		t.Error(err)
	})
	indexer.SetRetry(time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)
		indexer.Run(ctx)
	}()

	waitFor(t, "the backlog", func() bool {
		return index.upsertedCount() == 1200 && queue.size() == 0
	})

	queue.add("new")
	indexer.Notify("new")
	waitFor(t, "the new person", func() bool {
		return index.upsertedCount() == 1201
	})

	cancel()
	<-done

	if index.configured != 1 {
		t.Fatalf("configured %d times", index.configured)
	}
}

func TestTheIndexerKeepsTheQueueWhileTheIndexIsDown(t *testing.T) {
	t.Parallel()

	queue := &fakeQueue{}
	queue.add("anna")

	index := &fakeIndex{failUpsert: true}
	failures := make(chan error, 100)
	indexer := directory.NewIndexer(queue, index, func(err error) {
		select {
		case failures <- err:
		default:
		}
	})
	indexer.SetRetry(5 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go indexer.Run(ctx)

	<-failures

	if queue.size() != 1 {
		t.Fatal("lost a person while the index was down")
	}

	index.setFailUpsert(false)
	waitFor(t, "the retry", func() bool {
		return queue.size() == 0 && index.upsertedCount() == 1
	})
}
