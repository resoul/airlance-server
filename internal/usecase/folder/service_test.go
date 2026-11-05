package folder_test

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "go.airlance.org/server/internal/domain/folder"
	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/profile"
	"go.airlance.org/server/internal/usecase/folder"
)

type memory struct {
	lists map[string]domain.List
}

func (m *memory) Get(_ context.Context, owner string) (domain.List, error) {
	if list, ok := m.lists[owner]; ok {
		return list, nil
	}

	return domain.Empty(), nil
}

func (m *memory) Put(_ context.Context, owner string, list domain.List, _ time.Time) (domain.List, error) {
	list.Revision = m.lists[owner].Revision + 1
	m.lists[owner] = list

	return list, nil
}

type bell struct {
	rung []string
}

func (b *bell) Notify(subject string) {
	b.rung = append(b.rung, subject)
}

func TestFoldersBelongToTheirOwnerAndWakeTheirDevices(t *testing.T) {
	t.Parallel()

	repository, notifier := &memory{lists: map[string]domain.List{}}, &bell{}
	service := folder.New(repository, notifier, time.Now)
	ctx := context.Background()
	anna := identity.Principal{Subject: "anna", Scopes: []string{profile.ScopeRead, profile.ScopeWrite}}
	petro := identity.Principal{Subject: "petro", Scopes: []string{profile.ScopeRead}}
	list := domain.List{Folders: []domain.Folder{{ID: "f", Name: "Работа", Color: 1}}, AllChatsPosition: 1, ShowsTags: true}

	saved, err := service.Put(ctx, anna, list)
	if err != nil || saved.Revision != 1 || len(notifier.rung) != 1 || notifier.rung[0] != "anna" {
		t.Fatalf("got %+v, %v, %v", saved, err, notifier.rung)
	}

	other, err := service.Get(ctx, petro)
	if err != nil || len(other.Folders) != 0 || other.Revision != 0 {
		t.Fatalf("someone else's folders: %+v, %v", other, err)
	}

	if _, err := service.Put(ctx, petro, list); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("read-only: %v", err)
	}

	if _, err := service.Get(ctx, identity.Principal{Subject: "anna"}); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("no scope: %v", err)
	}

	bad := domain.List{Folders: []domain.Folder{{ID: "f", Name: " "}}}
	if _, err := service.Put(ctx, anna, bad); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("a bad list: %v", err)
	}
}
