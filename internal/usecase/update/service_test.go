package update_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/profile"
	domain "go.airlance.org/server/internal/domain/update"
	"go.airlance.org/server/internal/usecase/update"
)

type emptyRepository struct{}

func (emptyRepository) Latest(context.Context, string) (uint64, error) {
	return 7, nil
}

func (emptyRepository) After(context.Context, string, uint64, int) ([]domain.Update, error) {
	return nil, nil
}

func TestUpdatesNeedTheRightToReadTheProfile(t *testing.T) {
	t.Parallel()

	service := update.New(emptyRepository{}, update.NewHub())
	ctx := context.Background()
	writer := identity.Principal{Subject: "a", Scopes: []string{profile.ScopeWrite}}

	if _, err := service.Subscribe(ctx, writer, 0); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("subscribe without the scope: %v", err)
	}

	if _, err := service.After(ctx, writer, 0, 10); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("read without the scope: %v", err)
	}
}

func TestANumberTheDatabaseCannotHoldIsRefused(t *testing.T) {
	t.Parallel()

	service := update.New(emptyRepository{}, update.NewHub())
	reader := identity.Principal{Subject: "a", Scopes: []string{profile.ScopeRead}}

	if _, err := service.Subscribe(context.Background(), reader, domain.MaxSeq+1); !errors.Is(err, domain.ErrInvalidSeq) {
		t.Fatalf("got %v", err)
	}

	latest, err := service.Subscribe(context.Background(), reader, domain.MaxSeq)
	if err != nil || latest != 7 {
		t.Fatalf("got %d, %v", latest, err)
	}
}

func TestAHubWakesOnlyThatPerson(t *testing.T) {
	t.Parallel()

	hub := update.NewHub()
	anna, stopAnna := hub.Listen("anna")
	defer stopAnna()

	petro, stopPetro := hub.Listen("petro")
	defer stopPetro()

	hub.Notify("anna")
	hub.Notify("anna")

	select {
	case <-anna:
	case <-time.After(time.Second):
		t.Fatal("anna was not woken")
	}

	select {
	case <-petro:
		t.Fatal("petro was woken")
	default:
	}
}

func TestAHubDropsAListenerThatStopped(t *testing.T) {
	t.Parallel()

	hub := update.NewHub()
	listener, stop := hub.Listen("anna")
	stop()
	hub.Notify("anna")

	select {
	case <-listener:
		t.Fatal("woken after stopping")
	default:
	}
}
