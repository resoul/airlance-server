package message_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.airlance.org/server/internal/domain/identity"
	domain "go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/profile"
	"go.airlance.org/server/internal/usecase/message"
)

type fakeRepository struct {
	sent      map[string]domain.Message
	members   []string
	failing   bool
	lastLimit int
}

func (f *fakeRepository) OpenDirect(_ context.Context, candidate, _, _ string, _ time.Time) (string, error) {
	return candidate, nil
}

func (f *fakeRepository) Send(_ context.Context, next domain.Message) (domain.Message, []string, bool, error) {
	if f.failing {
		return domain.Message{}, nil, false, errors.New("database is down")
	}

	if stored, ok := f.sent[next.ClientID]; ok {
		return stored, f.members, false, nil
	}

	f.sent[next.ClientID] = next

	return next, f.members, true, nil
}

func (f *fakeRepository) History(_ context.Context, chatID, _ string, _ int64, limit int) ([]domain.Message, error) {
	f.lastLimit = limit
	if chatID == "foreign" {
		return nil, domain.ErrChatNotFound
	}

	return nil, nil
}

func (f *fakeRepository) Chats(context.Context, string, int64, int) ([]domain.Chat, error) {
	return []domain.Chat{{ID: "c"}}, nil
}

type fakeProfiles struct{}

func (fakeProfiles) Get(_ context.Context, subject string) (profile.Profile, error) {
	if subject == "missing" {
		return profile.Profile{}, profile.ErrNotFound
	}

	return profile.Profile{Subject: subject, Fields: profile.Fields{FirstName: "Петро", Username: "petro"}}, nil
}

type counter struct {
	woken map[string]int
}

func (c *counter) Notify(subject string) {
	c.woken[subject]++
}

var (
	both  = []string{domain.ScopeRead, domain.ScopeWrite}
	anna  = identity.Principal{Subject: "anna", Scopes: both}
	clock = func() time.Time {
		return time.UnixMilli(1_800_000_000_000)
	}
)

type openGuard struct{}

func (openGuard) CheckOpen(context.Context, string, string) error {
	return nil
}

func (openGuard) Filter(_ context.Context, _ string, target profile.Profile) (profile.Profile, error) {
	return target, nil
}

func newService() (*message.Service, *fakeRepository, *counter) {
	repository := &fakeRepository{sent: map[string]domain.Message{}, members: []string{"anna", "petro"}}
	notifier := &counter{woken: map[string]int{}}

	return message.New(repository, fakeProfiles{}, openGuard{}, notifier, message.NewIDs(0, clock), clock), repository, notifier
}

func TestEveryRequestNeedsItsScope(t *testing.T) {
	t.Parallel()

	service, _, _ := newService()
	ctx := context.Background()
	reader := identity.Principal{Subject: "anna", Scopes: []string{domain.ScopeRead}}
	writer := identity.Principal{Subject: "anna", Scopes: []string{domain.ScopeWrite}}

	if _, err := service.OpenDirect(ctx, reader, "petro"); !errors.Is(err, identity.ErrForbidden) {
		t.Errorf("open: %v", err)
	}

	if _, err := service.Send(ctx, reader, "c", "l", "hi"); !errors.Is(err, identity.ErrForbidden) {
		t.Errorf("send: %v", err)
	}

	if _, err := service.History(ctx, writer, "c", 0, 10); !errors.Is(err, identity.ErrForbidden) {
		t.Errorf("history: %v", err)
	}

	if _, err := service.Chats(ctx, writer, 0); !errors.Is(err, identity.ErrForbidden) {
		t.Errorf("chats: %v", err)
	}
}

func TestADirectChatNeedsARealOtherPerson(t *testing.T) {
	t.Parallel()

	service, _, _ := newService()
	ctx := context.Background()

	for _, peer := range []string{"", "anna"} {
		if _, err := service.OpenDirect(ctx, anna, peer); !errors.Is(err, domain.ErrInvalidPeer) {
			t.Errorf("%q: %v", peer, err)
		}
	}

	if _, err := service.OpenDirect(ctx, anna, "missing"); !errors.Is(err, domain.ErrPeerNotFound) {
		t.Errorf("a missing person: %v", err)
	}

	chat, err := service.OpenDirect(ctx, anna, "petro")
	if err != nil || chat.Peer.Username != "petro" || len(chat.ID) != 32 {
		t.Fatalf("got %+v, %v", chat, err)
	}
}

func TestSendingWakesEveryMemberOnlyTheFirstTime(t *testing.T) {
	t.Parallel()

	service, _, notifier := newService()
	ctx := context.Background()

	first, err := service.Send(ctx, anna, "c", "local-1", "привет")
	if err != nil {
		t.Fatal(err)
	}

	if notifier.woken["anna"] != 1 || notifier.woken["petro"] != 1 {
		t.Fatalf("woken %v", notifier.woken)
	}

	again, err := service.Send(ctx, anna, "c", "local-1", "привет")
	if err != nil || again.ID != first.ID {
		t.Fatalf("a repeat: %+v, %v", again, err)
	}

	if notifier.woken["petro"] != 1 {
		t.Fatalf("a repeat woke them: %v", notifier.woken)
	}
}

func TestARefusedMessageReachesNobody(t *testing.T) {
	t.Parallel()

	service, repository, notifier := newService()
	ctx := context.Background()

	if _, err := service.Send(ctx, anna, "c", "local", "  "); !errors.Is(err, domain.ErrInvalidText) {
		t.Fatalf("got %v", err)
	}

	repository.failing = true
	if _, err := service.Send(ctx, anna, "c", "local", "hi"); err == nil {
		t.Fatal("expected an error")
	}

	if len(repository.sent) != 0 || len(notifier.woken) != 0 {
		t.Fatalf("sent %v, woken %v", repository.sent, notifier.woken)
	}
}

func TestAHistoryPageIsBounded(t *testing.T) {
	t.Parallel()

	service, repository, _ := newService()
	ctx := context.Background()

	for ask, want := range map[int]int{0: 100, -1: 100, 101: 100, 20: 20} {
		if _, err := service.History(ctx, anna, "c", 0, ask); err != nil {
			t.Fatal(err)
		}

		if repository.lastLimit != want {
			t.Errorf("asked %d, used %d", ask, repository.lastLimit)
		}
	}

	if _, err := service.History(ctx, anna, "foreign", 0, 10); !errors.Is(err, domain.ErrChatNotFound) {
		t.Fatalf("a foreign chat: %v", err)
	}
}

func TestIDsAlwaysGrow(t *testing.T) {
	t.Parallel()

	at := int64(1_800_000_000_000)
	ids := message.NewIDs(3, func() time.Time {
		return time.UnixMilli(at)
	})

	previous := ids.Next()
	if previous <= 0 {
		t.Fatalf("got %d", previous)
	}

	for range 20_000 {
		next := ids.Next()
		if next <= previous {
			t.Fatalf("%d after %d in one millisecond", next, previous)
		}

		previous = next
	}

	at -= 60_000
	if next := ids.Next(); next <= previous {
		t.Fatalf("%d after %d when the clock went back", next, previous)
	}

	at += 3_600_000
	if next := ids.Next(); next <= previous {
		t.Fatalf("%d after %d when the clock went on", next, previous)
	}
}
