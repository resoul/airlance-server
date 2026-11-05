package contact_test

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "go.airlance.org/server/internal/domain/contact"
	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/privacy"
	"go.airlance.org/server/internal/domain/profile"
	"go.airlance.org/server/internal/usecase/contact"
)

type memory struct {
	added  []contact.Added
	phones map[string][]string
	synced []string
}

func (m *memory) Add(_ context.Context, next contact.Added) error {
	m.added = append(m.added, next)

	return nil
}

func (m *memory) Remove(context.Context, string, string) error {
	return nil
}

func (m *memory) List(context.Context, string) ([]domain.Contact, error) {
	return []domain.Contact{{Subject: "boris"}}, nil
}

func (m *memory) FindByPhone(_ context.Context, _, digits string) ([]string, error) {
	return m.phones[digits], nil
}

func (m *memory) Sync(_ context.Context, _ string, phones []string, _ time.Time) (map[string]string, error) {
	m.synced = phones

	return map[string]string{"boris": "380674204830"}, nil
}

type relations struct {
	blocked bool
}

func (r relations) Relation(context.Context, string, string) (privacy.Relation, error) {
	return privacy.Relation{BlockedByTarget: r.blocked}, nil
}

type profiles struct{}

func (profiles) Get(_ context.Context, subject string) (profile.Profile, error) {
	if subject == "missing" {
		return profile.Profile{}, profile.ErrNotFound
	}

	return profile.Profile{Subject: subject}, nil
}

type notifier struct {
	count int
}

func (n *notifier) Notify(string) {
	n.count++
}

var anna = identity.Principal{Subject: "anna", Scopes: []string{profile.ScopeRead, profile.ScopeWrite}}

func newService(blocked bool) (*contact.Service, *memory, *notifier) {
	repository := &memory{phones: map[string][]string{"380674204830": {"boris"}}}
	wakes := &notifier{}
	now := time.Unix(100, 0)

	return contact.New(repository, relations{blocked: blocked}, profiles{}, wakes, func() time.Time {
		return now
	}), repository, wakes
}

func TestAddChecksTheTarget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, repository, wakes := newService(false)

	if _, err := service.Add(ctx, anna, "boris", " Боря ", ""); err != nil {
		t.Fatal(err)
	}

	if repository.added[0].CustomFirstName != "Боря" || wakes.count != 1 {
		t.Fatalf("added %+v, woke %d", repository.added, wakes.count)
	}

	if _, err := service.Add(ctx, anna, "anna", "", ""); !errors.Is(err, domain.ErrInvalidPeer) {
		t.Fatalf("yourself: %v", err)
	}

	if _, err := service.Add(ctx, anna, "missing", "", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a missing person: %v", err)
	}

	blocked, _, _ := newService(true)
	if _, err := blocked.Add(ctx, anna, "boris", "", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("blocked by them: %v", err)
	}
}

func TestAddByPhoneFindsOnlyWhoIsThere(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, repository, _ := newService(false)

	if _, err := service.AddByPhone(ctx, anna, "+380 67 420 48 30", "", ""); err != nil {
		t.Fatal(err)
	}

	if len(repository.added) != 1 || repository.added[0].Subject != "boris" {
		t.Fatalf("added %+v", repository.added)
	}

	if _, err := service.AddByPhone(ctx, anna, "+1 555 000 11 22", "", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a free number: %v", err)
	}

	if _, err := service.AddByPhone(ctx, anna, "12", "", ""); !errors.Is(err, domain.ErrInvalidPhone) {
		t.Fatalf("a bad number: %v", err)
	}
}

func TestSyncCleansNumbersAndGivesTheMatch(t *testing.T) {
	t.Parallel()

	service, repository, _ := newService(false)

	contacts, err := service.Sync(context.Background(), anna, []string{"+380 67 420 48 30", "bad"})
	if err != nil {
		t.Fatal(err)
	}

	if len(repository.synced) != 1 || contacts[0].Phone != "380674204830" {
		t.Fatalf("synced %v, got %+v", repository.synced, contacts)
	}

	tooMany := make([]string, domain.MaxSyncPhones+1)
	if _, err := service.Sync(context.Background(), anna, tooMany); !errors.Is(err, domain.ErrTooManyPhones) {
		t.Fatalf("too many: %v", err)
	}
}

func TestLookupsAreLimitedPerPerson(t *testing.T) {
	t.Parallel()

	service, _, _ := newService(false)
	ctx := context.Background()

	for range 30 {
		if _, err := service.Sync(ctx, anna, nil); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := service.Sync(ctx, anna, nil); !errors.Is(err, domain.ErrTooManyTries) {
		t.Fatalf("over the limit: %v", err)
	}

	boris := identity.Principal{Subject: "boris", Scopes: anna.Scopes}
	if _, err := service.Sync(ctx, boris, nil); err != nil {
		t.Fatalf("another person: %v", err)
	}
}
