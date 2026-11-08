//go:build integration

package postgres_test

import (
	"errors"
	"slices"
	"testing"

	domain "go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/update"
	"go.airlance.org/server/internal/repository/postgres"
	usecase "go.airlance.org/server/internal/usecase/message"
)

func (s *stage) channel(t *testing.T, owner string, people ...string) domain.Chat {
	t.Helper()

	chat, err := s.messages.Create(s.ctx, person(owner), usecase.NewChat{
		ClientID: "ch-" + owner, Kind: domain.KindChannel, Title: "Новости", People: people,
	})
	if err != nil {
		t.Fatal(err)
	}

	return chat
}

func TestPinsAreSharedAndKeepTheirRules(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "anna", "petro")
	chat := s.group(t, "anna", "petro")

	first, err := s.messages.Send(s.ctx, person("petro"), chat.ID, "one", "важное")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.messages.SetPin(s.ctx, person("petro"), chat.ID, first.ID, true); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a member pinned in a group: %v", err)
	}

	pins, err := s.messages.SetPin(s.ctx, person("anna"), chat.ID, first.ID, true)
	if err != nil || !slices.Equal(pins, []int64{first.ID}) {
		t.Fatalf("got %v, %v", pins, err)
	}

	if _, err := s.messages.SetPin(s.ctx, person("anna"), chat.ID, 7, true); !errors.Is(err, domain.ErrMessageMissing) {
		t.Fatalf("a missing message was pinned: %v", err)
	}

	seen, err := s.messages.Pins(s.ctx, person("petro"), chat.ID)
	if err != nil || !slices.Equal(seen, []int64{first.ID}) {
		t.Fatalf("another member sees %v, %v", seen, err)
	}

	journal, err := postgres.NewUpdates(s.pool).After(s.ctx, "petro", 0, 100)
	if err != nil || journal[len(journal)-1].Kind != update.KindPins || !slices.Equal(journal[len(journal)-1].Pins, pins) {
		t.Fatalf("the other member was not told: %+v, %v", journal, err)
	}

	if _, err := s.messages.Delete(s.ctx, person("anna"), chat.ID, []int64{first.ID}, true); err != nil {
		t.Fatal(err)
	}

	if seen, _ := s.messages.Pins(s.ctx, person("petro"), chat.ID); len(seen) != 0 {
		t.Fatalf("a deleted message stayed pinned: %v", seen)
	}
}

func TestChannelViewsCountEachPersonOnce(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "anna", "petro", "olena")
	chat := s.channel(t, "anna", "petro", "olena")

	post, err := s.messages.Send(s.ctx, person("anna"), chat.ID, "post", "новость")
	if err != nil {
		t.Fatal(err)
	}

	for _, who := range []string{"petro", "petro", "olena"} {
		if _, err := s.messages.PostStats(s.ctx, person(who), chat.ID, []int64{post.ID}, true); err != nil {
			t.Fatal(err)
		}
	}

	stats, err := s.messages.PostStats(s.ctx, person("anna"), chat.ID, []int64{post.ID}, false)
	if err != nil || len(stats) != 1 || stats[0].Views != 2 {
		t.Fatalf("got %+v, %v", stats, err)
	}

	if _, err := s.messages.PostStats(s.ctx, person("stranger"), chat.ID, []int64{post.ID}, true); !errors.Is(err, domain.ErrChatNotFound) {
		t.Fatalf("a stranger counted a view: %v", err)
	}
}

func TestCommentsLiveUnderThePostAndOnlyReachTheirAuthors(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "anna", "petro", "olena", "ivan")
	chat := s.channel(t, "anna", "petro", "olena", "ivan")

	post, err := s.messages.Send(s.ctx, person("anna"), chat.ID, "post", "новость")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.messages.SendContent(s.ctx, person("petro"), chat.ID, "x", domain.Outgoing{Text: "нельзя"}); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a subscriber posted: %v", err)
	}

	first, err := s.messages.SendContent(s.ctx, person("petro"), chat.ID, "c1", domain.Outgoing{Text: "первый", ThreadID: post.ID})
	if err != nil || first.ThreadID != post.ID {
		t.Fatalf("got %+v, %v", first, err)
	}

	if _, err := s.messages.SendContent(s.ctx, person("olena"), chat.ID, "c2", domain.Outgoing{Text: "второй", ThreadID: post.ID}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.messages.SendContent(s.ctx, person("olena"), chat.ID, "c3", domain.Outgoing{Text: "мимо", ThreadID: 5}); !errors.Is(err, domain.ErrInvalidThread) {
		t.Fatalf("a comment under a missing post: %v", err)
	}

	if got := s.history(t, "ivan", chat.ID); len(got) != 1 {
		t.Fatalf("comments leaked into the channel: %+v", got)
	}

	comments, err := s.messages.Thread(s.ctx, person("ivan"), chat.ID, post.ID, 0, 50)
	if err != nil || len(comments) != 2 {
		t.Fatalf("got %+v, %v", comments, err)
	}

	stats, err := s.messages.PostStats(s.ctx, person("ivan"), chat.ID, []int64{post.ID}, false)
	if err != nil || stats[0].Comments != 2 || len(stats[0].Recent) != 2 || stats[0].Recent[0].Subject != "olena" {
		t.Fatalf("got %+v, %v", stats, err)
	}

	journal, err := postgres.NewUpdates(s.pool).After(s.ctx, "ivan", 0, 100)
	if err != nil {
		t.Fatal(err)
	}

	for _, next := range journal {
		if next.Kind == update.KindMessage && next.Message.ThreadID != 0 {
			t.Fatalf("a reader who never commented got a comment: %+v", next)
		}
	}

	chats, err := s.messages.Chats(s.ctx, person("ivan"), 0)
	if err != nil || chats[0].Last == nil || chats[0].Last.ID != post.ID {
		t.Fatalf("a comment became the last message: %+v, %v", chats, err)
	}
}
