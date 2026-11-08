//go:build integration

package postgres_test

import (
	"errors"
	"testing"

	domain "go.airlance.org/server/internal/domain/message"
)

func TestChatLogRecordsWhatAdminsDoAndOnlyAdminsReadIt(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "owner", "petro", "kira", "mila")
	chat := s.group(t, "owner", "petro", "kira", "mila")

	info := domain.ChatInfo{Title: "Новое имя", About: "О чём", Link: "log_group_link"}
	if _, err := s.messages.SetInfo(s.ctx, person("owner"), chat.ID, info); err != nil {
		t.Fatal(err)
	}

	if _, err := s.messages.SetInfo(s.ctx, person("owner"), chat.ID, info); err != nil {
		t.Fatalf("a repeat: %v", err)
	}

	rules := domain.ChatRules{NoCopy: true, Signs: true, ReactionsMode: domain.ReactionsNone}
	if _, err := s.messages.SetRules(s.ctx, person("owner"), chat.ID, rules); err != nil {
		t.Fatal(err)
	}

	if _, err := s.messages.AddMember(s.ctx, person("owner"), chat.ID, "kira", domain.RoleAdmin, domain.RightBan); err != nil {
		t.Fatal(err)
	}

	if _, err := s.messages.RemoveMember(s.ctx, person("owner"), chat.ID, "petro", true); err != nil {
		t.Fatal(err)
	}

	if _, err := s.messages.Unban(s.ctx, person("owner"), chat.ID, "petro"); err != nil {
		t.Fatal(err)
	}

	if _, err := s.messages.Unban(s.ctx, person("owner"), chat.ID, "petro"); err != nil {
		t.Fatalf("an unban of nobody: %v", err)
	}

	entries, err := s.messages.ChatLog(s.ctx, person("kira"), chat.ID)
	if err != nil {
		t.Fatal(err)
	}

	want := []domain.LogKind{
		domain.LogUnbanned, domain.LogBanned, domain.LogAdminAdded, domain.LogReactionsChanged,
		domain.LogSignaturesOn, domain.LogCopyingRestricted, domain.LogMadePublic, domain.LogAboutChanged,
		domain.LogRenamed,
	}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries: %+v", len(entries), entries)
	}

	for index, kind := range want {
		if entries[index].Kind != kind {
			t.Fatalf("entry %d is %d, want %d", index, entries[index].Kind, kind)
		}
	}

	if entries[0].Detail == "" || entries[0].Actor.Subject != "owner" {
		t.Fatalf("the unban lost its target or actor: %+v", entries[0])
	}

	if _, err := s.messages.ChatLog(s.ctx, person("mila"), chat.ID); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a member read the log: %v", err)
	}
}
