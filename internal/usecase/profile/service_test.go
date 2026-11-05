package profile_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.airlance.org/server/internal/domain/identity"
	domain "go.airlance.org/server/internal/domain/profile"
	"go.airlance.org/server/internal/usecase/profile"
)

type memoryRepository struct {
	saved map[string]domain.Profile
}

func (m *memoryRepository) Get(_ context.Context, subject string) (domain.Profile, error) {
	got, ok := m.saved[subject]
	if !ok {
		return domain.Profile{}, domain.ErrNotFound
	}

	return got, nil
}

func (m *memoryRepository) Save(_ context.Context, next domain.Profile) error {
	m.saved[next.Subject] = next

	return nil
}

type countingNotifier struct {
	notified map[string]int
}

func (n *countingNotifier) Notify(subject string) {
	n.notified[subject]++
}

func newService() (*profile.Service, *memoryRepository) {
	service, repository, _ := newNotifyingService()

	return service, repository
}

func newNotifyingService() (*profile.Service, *memoryRepository, *countingNotifier) {
	repository := &memoryRepository{saved: map[string]domain.Profile{}}
	notifier := &countingNotifier{notified: map[string]int{}}
	now := time.Unix(100, 0)

	return profile.New(repository, notifier, func() time.Time {
		return now
	}), repository, notifier
}

func TestOnlyASavedChangeWakesTheOwner(t *testing.T) {
	t.Parallel()

	service, _, notifier := newNotifyingService()
	principal := identity.Principal{Subject: "anna", Scopes: []string{domain.ScopeWrite}}

	if _, err := service.UpdateOwn(context.Background(), principal, domain.Fields{}); err == nil {
		t.Fatal("an invalid profile was accepted")
	}

	if notifier.notified["anna"] != 0 {
		t.Fatal("woken by a refused change")
	}

	if _, err := service.UpdateOwn(context.Background(), principal, domain.Fields{FirstName: "Анна"}); err != nil {
		t.Fatal(err)
	}

	if notifier.notified["anna"] != 1 {
		t.Fatalf("got %d wake-ups", notifier.notified["anna"])
	}
}

func TestScopesDecideWhatAPersonMayDo(t *testing.T) {
	t.Parallel()

	service, _ := newService()
	ctx := context.Background()
	reader := identity.Principal{Subject: "a", Scopes: []string{domain.ScopeRead}}
	writer := identity.Principal{Subject: "a", Scopes: []string{domain.ScopeWrite}}

	if _, err := service.GetOwn(ctx, writer); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("read without the scope: %v", err)
	}

	if _, err := service.UpdateOwn(ctx, reader, domain.Fields{FirstName: "A"}); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("write without the scope: %v", err)
	}
}

func TestAPersonOnlyEverTouchesTheirOwnProfile(t *testing.T) {
	t.Parallel()

	service, repository := newService()
	ctx := context.Background()
	scopes := []string{domain.ScopeRead, domain.ScopeWrite}
	anna := identity.Principal{Subject: "anna", Scopes: scopes}
	petro := identity.Principal{Subject: "petro", Scopes: scopes}

	if _, err := service.UpdateOwn(ctx, anna, domain.Fields{FirstName: "Анна"}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.GetOwn(ctx, petro); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("another person's profile: %v", err)
	}

	if repository.saved["anna"].UpdatedAt.Unix() != 100 {
		t.Fatalf("the clock was not used: %+v", repository.saved["anna"])
	}
}

func TestAPublicProfileNeedsTheRightToRead(t *testing.T) {
	t.Parallel()

	service, _ := newService()
	ctx := context.Background()
	anna := identity.Principal{Subject: "anna", Scopes: []string{domain.ScopeWrite}}
	petro := identity.Principal{Subject: "petro", Scopes: []string{domain.ScopeRead}}

	if _, err := service.UpdateOwn(ctx, anna, domain.Fields{FirstName: "Анна"}); err != nil {
		t.Fatal(err)
	}

	got, err := service.GetPublic(ctx, petro, "anna")
	if err != nil || got.Fields.FirstName != "Анна" {
		t.Fatalf("got %+v, %v", got, err)
	}

	if _, err := service.GetPublic(ctx, anna, "anna"); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("without the scope: %v", err)
	}

	if _, err := service.GetPublic(ctx, petro, "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a missing person: %v", err)
	}
}

func TestEveryNotifierIsWoken(t *testing.T) {
	t.Parallel()

	first := &countingNotifier{notified: map[string]int{}}
	second := &countingNotifier{notified: map[string]int{}}

	profile.Notifiers{first, second}.Notify("anna")

	if first.notified["anna"] != 1 || second.notified["anna"] != 1 {
		t.Fatalf("got %v and %v", first.notified, second.notified)
	}
}

func TestAnInvalidProfileIsNotSaved(t *testing.T) {
	t.Parallel()

	service, repository := newService()
	principal := identity.Principal{Subject: "a", Scopes: []string{domain.ScopeWrite}}

	_, err := service.UpdateOwn(context.Background(), principal, domain.Fields{})
	if !errors.Is(err, domain.ErrInvalidName) {
		t.Fatalf("got %v", err)
	}

	if len(repository.saved) != 0 {
		t.Fatal("saved anyway")
	}
}
