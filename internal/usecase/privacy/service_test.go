package privacy_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.airlance.org/server/internal/domain/directory"
	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/message"
	domain "go.airlance.org/server/internal/domain/privacy"
	"go.airlance.org/server/internal/domain/profile"
	"go.airlance.org/server/internal/usecase/privacy"
)

type memory struct {
	settings domain.Settings
	relation domain.Relation
	blocked  map[string]bool
}

func (m *memory) Settings(context.Context, string) (domain.Settings, error) {
	return m.settings, nil
}

func (m *memory) SaveSettings(_ context.Context, _ string, settings domain.Settings) error {
	m.settings = settings

	return nil
}

func (m *memory) Relation(context.Context, string, string) (domain.Relation, error) {
	return m.relation, nil
}

func (m *memory) SetBlocked(_ context.Context, _, target string, blocked bool, _ time.Time) error {
	m.blocked[target] = blocked

	return nil
}

func (m *memory) Blocked(context.Context, string) ([]directory.Person, error) {
	var people []directory.Person

	for subject, blocked := range m.blocked {
		if blocked {
			people = append(people, directory.Person{Subject: subject})
		}
	}

	return people, nil
}

func (m *memory) BlockersOf(context.Context, string) ([]string, error) {
	return []string{"boris"}, nil
}

type notifier struct {
	notified []string
}

func (n *notifier) Notify(subject string) {
	n.notified = append(n.notified, subject)
}

var (
	anna = identity.Principal{Subject: "anna", Scopes: []string{profile.ScopeRead, profile.ScopeWrite}}
	full = profile.Profile{
		Subject: "boris",
		Fields:  profile.Fields{FirstName: "Б", Phone: "1", Bio: "b", PhotoMediaID: "p", VideoMediaID: "v"},
		Photos:  []profile.Photo{{PhotoMediaID: "p"}},
	}
)

func newService(settings domain.Settings, relation domain.Relation) (*privacy.Service, *memory, *notifier) {
	repository := &memory{settings: settings, relation: relation, blocked: map[string]bool{}}
	wakes := &notifier{}

	return privacy.New(repository, wakes, time.Now), repository, wakes
}

func TestFilterHidesWhatTheOwnerChose(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	closed := domain.Settings{Phone: domain.Nobody, Photos: domain.Nobody, Bio: domain.Nobody}
	service, _, _ := newService(closed, domain.Relation{IsContact: true})

	got, err := service.Filter(ctx, "anna", full)
	if err != nil {
		t.Fatal(err)
	}

	if got.Fields.Phone != "" || got.Fields.Bio != "" || got.Fields.PhotoMediaID != "" || got.Photos != nil {
		t.Fatalf("not hidden: %+v", got)
	}

	if got.Fields.FirstName != "Б" {
		t.Fatalf("the name went too: %+v", got)
	}

	own, err := service.Filter(ctx, "boris", full)
	if err != nil || own.Fields.Phone != "1" {
		t.Fatalf("own profile: %+v, %v", own, err)
	}

	contacts := domain.Settings{Phone: domain.Contacts, Photos: domain.Contacts, Bio: domain.Contacts}

	partner, _, _ := newService(contacts, domain.Relation{IsContact: true})
	if got, _ := partner.Filter(ctx, "anna", full); got.Fields.Phone != "1" || len(got.Photos) != 1 {
		t.Fatalf("a chat partner: %+v", got)
	}

	stranger, _, _ := newService(contacts, domain.Relation{})
	if got, _ := stranger.Filter(ctx, "anna", full); got.Fields.Phone != "" || got.Photos != nil {
		t.Fatalf("a stranger: %+v", got)
	}

	blocked, _, _ := newService(domain.Defaults(), domain.Relation{BlockedByTarget: true})
	if _, err := blocked.Filter(ctx, "anna", full); !errors.Is(err, profile.ErrNotFound) {
		t.Fatalf("blocked by the owner: %v", err)
	}
}

func TestCheckOpenFollowsBlocksAndTheMessagesSetting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	contactsOnly := domain.Defaults()
	contactsOnly.Messages = domain.Contacts

	for name, tc := range map[string]struct {
		settings domain.Settings
		relation domain.Relation
		want     error
	}{
		"open to all":     {domain.Defaults(), domain.Relation{}, nil},
		"chats, stranger": {contactsOnly, domain.Relation{}, message.ErrRestricted},
		"chats, partner":  {contactsOnly, domain.Relation{IsContact: true}, nil},
		"blocked by them": {domain.Defaults(), domain.Relation{BlockedByTarget: true}, message.ErrPeerNotFound},
		"blocked by me":   {domain.Defaults(), domain.Relation{BlocksTarget: true}, message.ErrBlocked},
	} {
		service, _, _ := newService(tc.settings, tc.relation)
		if err := service.CheckOpen(ctx, "anna", "boris"); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestBlockingWakesBothAndRefusesYourself(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, _, wakes := newService(domain.Defaults(), domain.Relation{})

	people, err := service.SetBlocked(ctx, anna, "boris", true)
	if err != nil || len(people) != 1 || len(wakes.notified) != 2 {
		t.Fatalf("blocked %+v, woke %v, %v", people, wakes.notified, err)
	}

	if _, err := service.SetBlocked(ctx, anna, "anna", true); !errors.Is(err, domain.ErrInvalidPeer) {
		t.Fatalf("yourself: %v", err)
	}

	reader := identity.Principal{Subject: "anna", Scopes: []string{profile.ScopeRead}}
	if _, err := service.SetBlocked(ctx, reader, "boris", true); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("without the scope: %v", err)
	}

	bad := domain.Defaults()
	bad.Search = domain.Contacts

	if _, err := service.Set(ctx, anna, bad); !errors.Is(err, domain.ErrInvalidSettings) {
		t.Fatalf("bad settings: %v", err)
	}
}
