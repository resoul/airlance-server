//go:build integration

package postgres_test

import (
	"errors"
	"testing"

	"go.airlance.org/server/internal/domain/directory"
	domain "go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/repository/postgres"
	usecase "go.airlance.org/server/internal/usecase/message"
)

func TestAnInviteShowsThePrivateChatButNotItsMessages(t *testing.T) {
	ctx, pool := openGroups(t)
	saveProfiles(ctx, t, pool, "owner", "guest")
	service := messenger(t, pool)
	links := postgres.NewMessages(pool)

	chat, err := service.Create(ctx, person("owner"), usecase.NewChat{
		ClientID: "c1", Kind: domain.KindGroup, Title: "Семья", About: "Свои",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Send(ctx, person("owner"), chat.ID, "m1", "секрет"); err != nil {
		t.Fatal(err)
	}

	seen, err := links.ChatByCode(ctx, chat.InviteToken, "guest")
	if err != nil {
		t.Fatal(err)
	}

	if seen.Title != "Семья" || seen.About != "Свои" || seen.MemberCount != 1 || seen.IsMember {
		t.Fatalf("got %+v", seen)
	}

	if seen.Last != nil || seen.InviteToken != "" {
		t.Fatalf("a guest sees too much: %+v", seen)
	}

	anonymous, err := links.ChatByCode(ctx, chat.InviteToken, "")
	if err != nil || anonymous.Last != nil || anonymous.InviteToken != "" {
		t.Fatalf("got %+v, %v", anonymous, err)
	}

	own, err := links.ChatByCode(ctx, chat.InviteToken, "owner")
	if err != nil || own.Last == nil || own.InviteToken == "" {
		t.Fatalf("the owner: %+v, %v", own, err)
	}

	if _, err := links.ChatByCode(ctx, "no_such_code", "guest"); !errors.Is(err, domain.ErrChatNotFound) {
		t.Fatalf("got %v", err)
	}

	if _, err := links.ChatByCode(ctx, "", "guest"); !errors.Is(err, domain.ErrChatNotFound) {
		t.Fatalf("an empty code: %v", err)
	}
}

func TestResettingAnInviteKillsTheOldOne(t *testing.T) {
	ctx, pool := openGroups(t)
	saveProfiles(ctx, t, pool, "owner", "member", "guest")
	service := messenger(t, pool)

	chat, err := service.Create(ctx, person("owner"), usecase.NewChat{
		ClientID: "c1", Kind: domain.KindChannel, Title: "Закрытый", People: []string{"member"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.ResetInvite(ctx, person("member"), chat.ID); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a subscriber reset the invite: %v", err)
	}

	fresh, err := service.ResetInvite(ctx, person("owner"), chat.ID)
	if err != nil || fresh.InviteToken == "" || fresh.InviteToken == chat.InviteToken {
		t.Fatalf("got %+v, %v", fresh, err)
	}

	if _, err := service.Join(ctx, person("guest"), chat.InviteToken); !errors.Is(err, domain.ErrChatNotFound) {
		t.Fatalf("the old invite still works: %v", err)
	}

	if _, err := service.Join(ctx, person("guest"), fresh.InviteToken); err != nil {
		t.Fatal(err)
	}

	public, err := service.Create(ctx, person("owner"), usecase.NewChat{
		ClientID: "c2", Kind: domain.KindChannel, Title: "Открытый", Link: "open_channel",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.ResetInvite(ctx, person("owner"), public.ID); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a public chat has no invite: %v", err)
	}
}

func TestAUsernameLeadsOnlyToWhoAllowsIt(t *testing.T) {
	ctx, pool := openGroups(t)
	saveProfiles(ctx, t, pool, "anna", "mila", "viewer")
	links := postgres.NewMessages(pool)

	for subject, username := range map[string]string{"anna": "anna_public", "mila": "mila_hidden"} {
		_, err := pool.Exec(ctx, `UPDATE profiles SET username = $2 WHERE subject = $1`, subject, username)
		if err != nil {
			t.Fatal(err)
		}
	}

	_, err := pool.Exec(ctx, `INSERT INTO profile_privacy (subject, search) VALUES ('mila', 2)`)
	if err != nil {
		t.Fatal(err)
	}

	found, err := links.PersonByUsername(ctx, "ANNA_public", "")
	if err != nil || found.Subject != "anna" {
		t.Fatalf("got %+v, %v", found, err)
	}

	if _, err := links.PersonByUsername(ctx, "mila_hidden", "viewer"); !errors.Is(err, directory.ErrNotFound) {
		t.Fatalf("a hidden person was found: %v", err)
	}

	if _, err := links.PersonByUsername(ctx, "nobody_here", "viewer"); !errors.Is(err, directory.ErrNotFound) {
		t.Fatalf("got %v", err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO blocks (owner, blocked, created_at) VALUES ('anna', 'viewer', now())`)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := links.PersonByUsername(ctx, "anna_public", "viewer"); !errors.Is(err, directory.ErrNotFound) {
		t.Fatalf("a blocker was found: %v", err)
	}

	if _, err := links.PersonByUsername(ctx, "anna_public", ""); err != nil {
		t.Fatalf("the public page: %v", err)
	}
}
