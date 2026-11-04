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

func newService() (*profile.Service, *memoryRepository) {
	repository := &memoryRepository{saved: map[string]domain.Profile{}}
	now := time.Unix(100, 0)

	return profile.New(repository, func() time.Time {
		return now
	}), repository
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
