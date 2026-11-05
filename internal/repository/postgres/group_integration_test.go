//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"go.airlance.org/server/internal/domain/contact"
	"go.airlance.org/server/internal/domain/directory"
	"go.airlance.org/server/internal/domain/identity"
	domain "go.airlance.org/server/internal/domain/message"
	domainprofile "go.airlance.org/server/internal/domain/profile"
	"go.airlance.org/server/internal/domain/update"
	"go.airlance.org/server/internal/repository/postgres"
	contacts "go.airlance.org/server/internal/usecase/contact"
	usecase "go.airlance.org/server/internal/usecase/message"
	"go.airlance.org/server/internal/usecase/privacy"
	"go.airlance.org/server/internal/usecase/profile"
	"go.airlance.org/server/internal/usecase/seed"
)

type silentNotifier struct{}

func (silentNotifier) Notify(string) {}

type memoryFiles struct{ puts, removes int }

func (f *memoryFiles) Put(context.Context, string, []byte) error {
	f.puts++

	return nil
}

func (f *memoryFiles) Remove(context.Context, string) error {
	f.removes++

	return nil
}

type memoryIndex struct{ hidden int }

func (i *memoryIndex) Upsert(_ context.Context, people []directory.Person) error {
	i.hidden += len(people)

	return nil
}

func openGroups(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()

	ctx, pool := openMessages(t)

	_, err := pool.Exec(ctx, "TRUNCATE contacts, blocks, profile_privacy, profile_photos, media_objects")
	if err != nil {
		t.Fatal(err)
	}

	return ctx, pool
}

func person(subject string) identity.Principal {
	return identity.Principal{Subject: subject, Scopes: []string{
		domainprofile.ScopeRead, domainprofile.ScopeWrite, domain.ScopeRead, domain.ScopeWrite,
	}}
}

func messenger(t *testing.T, pool *pgxpool.Pool) *usecase.Service {
	t.Helper()

	return messengerAt(pool, time.Now)
}

func messengerAt(pool *pgxpool.Pool, now func() time.Time) *usecase.Service {
	guard := privacy.New(postgres.NewPrivacy(pool), silentNotifier{}, time.Now)

	return usecase.New(
		postgres.NewMessages(pool), postgres.NewProfiles(pool), guard, silentNotifier{},
		usecase.NewIDs(0, now), now,
	)
}

func saveProfiles(ctx context.Context, t *testing.T, pool *pgxpool.Pool, subjects ...string) {
	t.Helper()

	for _, subject := range subjects {
		next := domainprofile.Profile{Subject: subject, Fields: domainprofile.Fields{FirstName: subject}, UpdatedAt: time.Now()}
		if err := postgres.NewProfiles(pool).Save(ctx, next); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAChannelOnlyLetsAdminsPostAndHidesWhoWrote(t *testing.T) {
	ctx, pool := openGroups(t)
	saveProfiles(ctx, t, pool, "owner", "admin", "reader", "stranger")
	service := messenger(t, pool)

	chat, err := service.Create(ctx, person("owner"), usecase.NewChat{
		ClientID: "c1", Kind: domain.KindChannel, Title: "Новости", Link: "seed_news_test", People: []string{"reader"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if chat.Role != domain.RoleOwner || chat.MemberCount != 2 || chat.InviteToken != "" {
		t.Fatalf("got %+v", chat)
	}

	if _, err := service.Send(ctx, person("reader"), chat.ID, "r1", "можно?"); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a subscriber posted: %v", err)
	}

	if _, err := service.AddMember(ctx, person("reader"), chat.ID, "admin", domain.RoleAdmin); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a subscriber made an admin: %v", err)
	}

	if _, err := service.AddMember(ctx, person("owner"), chat.ID, "admin", domain.RoleAdmin); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Send(ctx, person("admin"), chat.ID, "a1", "новость"); err != nil {
		t.Fatal(err)
	}

	read, err := service.History(ctx, person("reader"), chat.ID, 0, 10)
	if err != nil || len(read) != 1 {
		t.Fatalf("got %+v, %v", read, err)
	}

	if read[0].Sender != chat.ID || read[0].Author != (domain.Author{}) {
		t.Fatalf("a subscriber sees the author: %+v", read[0])
	}

	own, err := service.History(ctx, person("admin"), chat.ID, 0, 10)
	if err != nil || own[0].Sender != "admin" || own[0].ClientID != "a1" {
		t.Fatalf("the author lost their message: %+v, %v", own, err)
	}

	public, err := service.History(ctx, person("stranger"), chat.ID, 0, 10)
	if err != nil || len(public) != 1 {
		t.Fatalf("a public channel is not readable: %+v, %v", public, err)
	}

	members, err := service.Members(ctx, person("reader"), chat.ID)
	if err != nil || len(members) != 2 {
		t.Fatalf("a subscriber must see only admins: %+v, %v", members, err)
	}
}

func TestAPrivateChatStaysHiddenUntilInvited(t *testing.T) {
	ctx, pool := openGroups(t)
	saveProfiles(ctx, t, pool, "owner", "member", "guest")
	service := messenger(t, pool)

	chat, err := service.Create(ctx, person("owner"), usecase.NewChat{
		ClientID: "c1", Kind: domain.KindGroup, Title: "Семья", People: []string{"member", "missing"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if chat.MemberCount != 2 || chat.InviteToken == "" {
		t.Fatalf("got %+v", chat)
	}

	if _, err := service.History(ctx, person("guest"), chat.ID, 0, 10); !errors.Is(err, domain.ErrChatNotFound) {
		t.Fatalf("a stranger read a private chat: %v", err)
	}

	if _, err := service.Send(ctx, person("guest"), chat.ID, "g1", "привет"); !errors.Is(err, domain.ErrChatNotFound) {
		t.Fatalf("a stranger wrote: %v", err)
	}

	found, err := postgres.NewMessages(pool).SearchPublic(ctx, "guest", "Сем", 20)
	if err != nil || len(found) != 0 {
		t.Fatalf("search found a private chat: %+v, %v", found, err)
	}

	own, err := service.Join(ctx, person("member"), chat.InviteToken)
	if err != nil || own.InviteToken != "" {
		t.Fatalf("a member must not get the invite: %+v, %v", own, err)
	}

	joined, err := service.Join(ctx, person("guest"), chat.InviteToken)
	if err != nil || joined.MemberCount != 3 || !joined.IsMember {
		t.Fatalf("got %+v, %v", joined, err)
	}

	if _, err := service.Send(ctx, person("guest"), chat.ID, "g1", "привет"); err != nil {
		t.Fatal(err)
	}

	list, err := service.Chats(ctx, person("guest"), 0)
	if err != nil || len(list) != 1 || list[0].Title != "Семья" || list[0].Last.Author.FirstName != "guest" {
		t.Fatalf("got %+v, %v", list, err)
	}
}

func TestTheOwnerLeavingPassesTheChatOn(t *testing.T) {
	ctx, pool := openGroups(t)
	saveProfiles(ctx, t, pool, "owner", "first", "second")
	service := messenger(t, pool)

	chat, err := service.Create(ctx, person("owner"), usecase.NewChat{
		ClientID: "c1", Kind: domain.KindGroup, Title: "Группа", People: []string{"first", "second"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.RemoveMember(ctx, person("first"), chat.ID, "second"); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a member removed someone: %v", err)
	}

	if _, err := service.AddMember(ctx, person("owner"), chat.ID, "second", domain.RoleAdmin); err != nil {
		t.Fatal(err)
	}

	if err := service.Leave(ctx, person("owner"), chat.ID); err != nil {
		t.Fatal(err)
	}

	members, err := service.Members(ctx, person("second"), chat.ID)
	if err != nil || len(members) != 2 || members[0].Peer.Subject != "second" || members[0].Role != domain.RoleOwner {
		t.Fatalf("got %+v, %v", members, err)
	}

	if _, err := service.Chats(ctx, person("owner"), 0); err != nil {
		t.Fatal(err)
	}

	if _, err := service.History(ctx, person("owner"), chat.ID, 0, 10); !errors.Is(err, domain.ErrChatNotFound) {
		t.Fatalf("a former member still reads: %v", err)
	}
}

func TestALinkBelongsToOneChat(t *testing.T) {
	ctx, pool := openGroups(t)
	saveProfiles(ctx, t, pool, "owner")
	service := messenger(t, pool)

	first := usecase.NewChat{ClientID: "c1", Kind: domain.KindChannel, Title: "Один", Link: "same_link"}

	chat, err := service.Create(ctx, person("owner"), first)
	if err != nil {
		t.Fatal(err)
	}

	repeated, err := service.Create(ctx, person("owner"), first)
	if err != nil || repeated.ID != chat.ID {
		t.Fatalf("a repeat made another chat: %+v, %v", repeated, err)
	}

	second := usecase.NewChat{ClientID: "c2", Kind: domain.KindChannel, Title: "Два", Link: "same_link"}
	if _, err := service.Create(ctx, person("owner"), second); !errors.Is(err, domain.ErrLinkTaken) {
		t.Fatalf("got %v", err)
	}
}

func TestAGroupDoesNotOpenStoriesOrPrivacy(t *testing.T) {
	ctx, pool := openGroups(t)
	saveProfiles(ctx, t, pool, "owner", "member")
	service := messenger(t, pool)

	_, err := service.Create(ctx, person("owner"), usecase.NewChat{
		ClientID: "c1", Kind: domain.KindGroup, Title: "Группа", People: []string{"member"},
	})
	if err != nil {
		t.Fatal(err)
	}

	relation, err := postgres.NewPrivacy(pool).Relation(ctx, "owner", "member")
	if err != nil || relation.SharesChat {
		t.Fatalf("a group counted as a direct chat: %+v, %v", relation, err)
	}
}

func TestEveryoneHearsAboutAGroupAndAChannelAddsOnlyTheNewcomer(t *testing.T) {
	ctx, pool := openGroups(t)
	saveProfiles(ctx, t, pool, "owner", "member", "newcomer")
	service := messenger(t, pool)
	journal := postgres.NewUpdates(pool)

	for _, next := range []struct {
		kind domain.Kind
		id   string
		want int
	}{{domain.KindGroup, "g", 1}, {domain.KindChannel, "c", 0}} {
		chat, err := service.Create(ctx, person("owner"), usecase.NewChat{
			ClientID: next.id, Kind: next.kind, Title: next.id, Link: "chat_link_" + next.id, People: []string{"member"},
		})
		if err != nil {
			t.Fatal(err)
		}

		before, err := journal.Latest(ctx, "member")
		if err != nil {
			t.Fatal(err)
		}

		if _, err := service.Join(ctx, person("newcomer"), chat.Link); err != nil {
			t.Fatal(err)
		}

		found, err := journal.After(ctx, "member", before, 10)
		if err != nil || len(found) != next.want {
			t.Fatalf("%s: got %+v, %v", next.id, found, err)
		}

		for _, row := range found {
			if row.Kind != update.KindChats {
				t.Fatalf("got %+v", row)
			}
		}
	}
}

func newSeed(pool *pgxpool.Pool, files *memoryFiles, index *memoryIndex) *seed.Service {
	clock := &seed.Clock{}
	guard := privacy.New(postgres.NewPrivacy(pool), silentNotifier{}, time.Now)

	return seed.New(seed.Deps{
		Messaging: usecase.New(
			postgres.NewMessages(pool), postgres.NewProfiles(pool), guard, silentNotifier{},
			usecase.NewIDs(1, clock.Now), clock.Now,
		),
		Profiles: profile.New(postgres.NewProfiles(pool), postgres.NewMedia(pool), guard, silentNotifier{}, time.Now),
		Contacts: contacts.New(
			postgres.NewContacts(pool), postgres.NewPrivacy(pool), postgres.NewProfiles(pool), silentNotifier{}, time.Now,
		),
		Privacy:  guard,
		Media:    postgres.NewMedia(pool),
		Files:    files,
		Index:    index,
		Store:    postgres.NewSeeds(pool),
		Notifier: silentNotifier{},
		Clock:    clock,
		Print:    func(string) {},
	})
}

func TestSeedCanBeRepeatedAndCleanedWithoutBreakingTheJournal(t *testing.T) {
	ctx, pool := openGroups(t)
	saveProfiles(ctx, t, pool, "viewer")

	_, err := pool.Exec(ctx, `UPDATE profiles SET username = 'real_viewer' WHERE subject = 'viewer'`)
	if err != nil {
		t.Fatal(err)
	}

	files, index := &memoryFiles{}, &memoryIndex{}
	service := newSeed(pool, files, index)

	if err := service.Run(ctx, "@real_viewer"[1:]); err != nil {
		t.Fatal(err)
	}

	service = newSeed(pool, files, index)
	if err := service.Run(ctx, "real_viewer"); err != nil {
		t.Fatalf("a repeat: %v", err)
	}

	if files.puts < 12 {
		t.Fatalf("avatars uploaded %d times", files.puts)
	}

	chats, err := messenger(t, pool).Chats(ctx, person("viewer"), 0)
	if err != nil {
		t.Fatal(err)
	}

	kinds := map[domain.Kind]int{}
	for _, chat := range chats {
		kinds[chat.Kind]++
	}

	if kinds[domain.KindGroup] != 2 || kinds[domain.KindChannel] != 2 || kinds[domain.KindDirect] != 3 {
		t.Fatalf("got %v in %+v", kinds, chats)
	}

	var saved []contact.Contact

	saved, err = postgres.NewContacts(pool).List(ctx, "viewer")
	if err != nil || len(saved) != 3 {
		t.Fatalf("got %+v, %v", saved, err)
	}

	if err := service.Clean(ctx); err != nil {
		t.Fatal(err)
	}

	after, err := messenger(t, pool).Chats(ctx, person("viewer"), 0)
	if err != nil || len(after) != 0 {
		t.Fatalf("left behind %+v, %v", after, err)
	}

	found, err := postgres.NewUpdates(pool).After(ctx, "viewer", 0, 1000)
	if err != nil {
		t.Fatalf("the journal broke: %v", err)
	}

	for index, row := range found {
		if row.Seq != uint64(index+1) {
			t.Fatalf("a gap at %d: %+v", index, row)
		}
	}

	if !slices.ContainsFunc(found, func(row update.Update) bool {
		return row.Kind == update.KindChats
	}) {
		t.Fatal("no refresh in the journal")
	}

	var left int

	err = pool.QueryRow(ctx, `SELECT count(*) FROM profiles WHERE subject LIKE 'seed:%'`).Scan(&left)
	if err != nil || left != 0 || index.hidden != 12 || files.removes < 12 {
		t.Fatalf("left %d, %v, hidden %d, removed %d", left, err, index.hidden, files.removes)
	}
}
