//go:build integration

package postgres_test

import (
	"errors"
	"testing"

	domain "go.airlance.org/server/internal/domain/message"
)

func TestReadersListWhoReadMyMessageInAGroup(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "owner", "petro", "kira")
	chat := s.group(t, "owner", "petro", "kira")

	sent, err := s.messages.Send(s.ctx, person("owner"), chat.ID, "r1", "кто видел")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.messages.SetRead(s.ctx, person("petro"), chat.ID, sent.ID); err != nil {
		t.Fatal(err)
	}

	readers, err := s.messages.Readers(s.ctx, person("owner"), chat.ID, sent.ID)
	if err != nil || len(readers) != 1 || readers[0].Peer.Subject != "petro" {
		t.Fatalf("got %+v, %v", readers, err)
	}

	if _, err := s.messages.Readers(s.ctx, person("kira"), chat.ID, sent.ID); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("somebody else asked: %v", err)
	}

	if _, err := s.messages.Readers(s.ctx, person("owner"), chat.ID, sent.ID+100); !errors.Is(err, domain.ErrMessageMissing) {
		t.Fatalf("a missing message: %v", err)
	}

	news := s.channel(t, "owner", "petro")

	post, err := s.messages.Send(s.ctx, person("owner"), news.ID, "r2", "пост")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.messages.Readers(s.ctx, person("owner"), news.ID, post.ID); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a channel has no readers list: %v", err)
	}
}

func TestTypingReachesTheOthersOnlyWhereTheTypistMayWrite(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "owner", "petro", "kira")
	chat := s.group(t, "owner", "petro", "kira")

	who, audience, err := s.messages.Typing(s.ctx, person("petro"), chat.ID)
	if err != nil || who.Subject != "petro" || len(audience) != 2 {
		t.Fatalf("got %+v, %v, %v", who, audience, err)
	}

	if _, err := s.messages.SetMemberRights(s.ctx, person("owner"), chat.ID, 0); err != nil {
		t.Fatal(err)
	}

	if _, audience, err = s.messages.Typing(s.ctx, person("petro"), chat.ID); err != nil || len(audience) != 0 {
		t.Fatalf("a muted member is typing: %v, %v", audience, err)
	}

	news := s.channel(t, "owner", "petro")
	if _, audience, err = s.messages.Typing(s.ctx, person("owner"), news.ID); err != nil || len(audience) != 0 {
		t.Fatalf("a channel shows no typing: %v, %v", audience, err)
	}

	if _, _, err = s.messages.Typing(s.ctx, person("kira"), news.ID); !errors.Is(err, domain.ErrChatNotFound) {
		t.Fatalf("a stranger: %v", err)
	}
}
