//go:build integration

package postgres_test

import (
	"errors"
	"testing"

	domain "go.airlance.org/server/internal/domain/message"
)

func quizPoll() *domain.Poll {
	return &domain.Poll{Question: "Да?", Options: []domain.PollOption{{Text: "да"}, {Text: "нет"}}}
}

func TestMemberRightsDecideWhoWritesAndPins(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "anna", "petro")
	chat := s.group(t, "anna", "petro")

	if _, err := s.messages.Send(s.ctx, person("petro"), chat.ID, "a", "привет"); err != nil {
		t.Fatalf("a member writes by default: %v", err)
	}

	if _, err := s.messages.SetMemberRights(s.ctx, person("petro"), chat.ID, 0); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a member changed the rules: %v", err)
	}

	if _, err := s.messages.SetMemberRights(s.ctx, person("anna"), chat.ID, domain.RightSend); err != nil {
		t.Fatal(err)
	}

	if _, err := s.messages.SendContent(
		s.ctx, person("petro"), chat.ID, "p", domain.Outgoing{Poll: quizPoll()},
	); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a poll without the media right: %v", err)
	}

	sent, err := s.messages.Send(s.ctx, person("anna"), chat.ID, "b", "закреплю")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.messages.SetPin(s.ctx, person("petro"), chat.ID, sent.ID, true); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a member pinned: %v", err)
	}

	if _, err := s.messages.SetMemberRights(s.ctx, person("anna"), chat.ID, domain.RightPin); err != nil {
		t.Fatal(err)
	}

	if _, err := s.messages.Send(s.ctx, person("petro"), chat.ID, "c", "молчу"); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a muted member wrote: %v", err)
	}

	if _, err := s.messages.SetPin(s.ctx, person("petro"), chat.ID, sent.ID, true); err != nil {
		t.Fatalf("a member with the pin right: %v", err)
	}

	chats, err := s.messages.Chats(s.ctx, person("petro"), 0)
	if err != nil || chats[0].MyRights() != domain.RightPin || chats[0].MemberRights != domain.RightPin {
		t.Fatalf("got %+v, %v", chats, err)
	}
}

func TestAdminRightsAreCheckedOneByOne(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "owner", "adm", "petro", "kira")
	chat := s.group(t, "owner", "adm", "petro", "kira")

	if _, err := s.messages.AddMember(s.ctx, person("owner"), chat.ID, "adm", domain.RoleAdmin, domain.RightBan|domain.RightPromote); err != nil {
		t.Fatal(err)
	}

	wrote, err := s.messages.Send(s.ctx, person("petro"), chat.ID, "x", "чужое")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.messages.Delete(s.ctx, person("adm"), chat.ID, []int64{wrote.ID}, true); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("an admin without the delete right: %v", err)
	}

	if _, err := s.messages.AddMember(s.ctx, person("adm"), chat.ID, "petro", domain.RoleAdmin, domain.RightDelete); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("an admin gave a right they lack: %v", err)
	}

	if _, err := s.messages.AddMember(s.ctx, person("adm"), chat.ID, "petro", domain.RoleAdmin, domain.RightBan); err != nil {
		t.Fatalf("an admin gave a right they have: %v", err)
	}

	members, err := s.messages.Members(s.ctx, person("owner"), chat.ID)
	if err != nil {
		t.Fatal(err)
	}

	for _, member := range members {
		if member.Peer.Subject == "petro" && (member.Role != domain.RoleAdmin || member.Rights != domain.RightBan) {
			t.Fatalf("got %+v", member)
		}
	}

	if _, err := s.messages.AddMember(s.ctx, person("owner"), chat.ID, "petro", domain.RoleMember, 0); err != nil {
		t.Fatalf("demote: %v", err)
	}

	if _, err := s.messages.Delete(s.ctx, person("owner"), chat.ID, []int64{wrote.ID}, true); err != nil {
		t.Fatalf("the owner deletes: %v", err)
	}
}

func TestBansKeepPeopleOutUntilUnbanned(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "owner", "petro", "kira")
	chat := s.group(t, "owner", "petro", "kira")

	if _, err := s.messages.RemoveMember(s.ctx, person("kira"), chat.ID, "petro", false); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a member removed another: %v", err)
	}

	if _, err := s.messages.RemoveMember(s.ctx, person("owner"), chat.ID, "petro", true); err != nil {
		t.Fatal(err)
	}

	bans, err := s.messages.Bans(s.ctx, person("owner"), chat.ID)
	if err != nil || len(bans) != 1 || bans[0].Peer.Subject != "petro" {
		t.Fatalf("got %+v, %v", bans, err)
	}

	if _, err := s.messages.Bans(s.ctx, person("kira"), chat.ID); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a member read the bans: %v", err)
	}

	if _, err := s.messages.AddMember(s.ctx, person("owner"), chat.ID, "petro", domain.RoleMember, 0); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a banned person came back: %v", err)
	}

	if _, err := s.messages.Unban(s.ctx, person("owner"), chat.ID, "petro"); err != nil {
		t.Fatal(err)
	}

	if _, err := s.messages.AddMember(s.ctx, person("owner"), chat.ID, "petro", domain.RoleMember, 0); err != nil {
		t.Fatalf("after the unban: %v", err)
	}
}

func TestChatInfoNeedsTheInfoRight(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "owner", "petro")
	chat := s.group(t, "owner", "petro")

	info := domain.ChatInfo{Title: "Новое имя", About: "О чём", Link: "new_group_link"}
	if _, err := s.messages.SetInfo(s.ctx, person("petro"), chat.ID, info); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a member changed the info: %v", err)
	}

	changed, err := s.messages.SetInfo(s.ctx, person("owner"), chat.ID, info)
	if err != nil || changed.Title != "Новое имя" || changed.About != "О чём" || changed.Link != "new_group_link" ||
		changed.InviteToken != "" {
		t.Fatalf("got %+v, %v", changed, err)
	}

	if _, err := s.messages.SetMemberRights(s.ctx, person("owner"), chat.ID, domain.RightInfo); err != nil {
		t.Fatal(err)
	}

	info.Link = ""
	private, err := s.messages.SetInfo(s.ctx, person("petro"), chat.ID, info)
	if err != nil || private.Link != "" {
		t.Fatalf("got %+v, %v", private, err)
	}

	if _, err := s.messages.SetInfo(s.ctx, person("owner"), chat.ID, domain.ChatInfo{Title: " "}); !errors.Is(err, domain.ErrInvalidTitle) {
		t.Fatalf("an empty title: %v", err)
	}

	other := s.group(t, "owner")
	if _, err := s.messages.SetInfo(s.ctx, person("owner"), other.ID, domain.ChatInfo{Title: "Тот же", Link: "new_group_link"}); err != nil {
		t.Fatalf("a link released by making the first chat private: %v", err)
	}

	if _, err := s.messages.SetInfo(s.ctx, person("owner"), chat.ID, domain.ChatInfo{Title: "Новое имя", Link: "new_group_link"}); !errors.Is(err, domain.ErrLinkTaken) {
		t.Fatalf("a taken link: %v", err)
	}
}

func TestChatRulesLimitReactionsAndNeedTheInfoRight(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "owner", "petro", "olga")
	chat := s.group(t, "owner", "petro", "olga")

	post, err := s.messages.Send(s.ctx, person("owner"), chat.ID, "a", "пост")
	if err != nil {
		t.Fatal(err)
	}

	rules := domain.ChatRules{NoCopy: true, Signs: true, ReactionsMode: domain.ReactionsSome, Reactions: []string{"👍", "🔥", "👍"}, PerPost: 1}
	if _, err := s.messages.SetRules(s.ctx, person("petro"), chat.ID, rules); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a member changed the rules: %v", err)
	}

	saved, err := s.messages.SetRules(s.ctx, person("owner"), chat.ID, rules)
	if err != nil || !saved.Rules.NoCopy || !saved.Rules.Signs || saved.Rules.PerPost != 1 ||
		len(saved.Rules.Reactions) != 2 {
		t.Fatalf("got %+v, %v", saved.Rules, err)
	}

	if _, err := s.messages.SetReaction(s.ctx, person("petro"), chat.ID, post.ID, "🌶"); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("an emoji outside the list: %v", err)
	}

	if _, err := s.messages.SetReaction(s.ctx, person("petro"), chat.ID, post.ID, "👍"); err != nil {
		t.Fatal(err)
	}

	if _, err := s.messages.SetReaction(s.ctx, person("olga"), chat.ID, post.ID, "🔥"); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a second kind under a limit of one: %v", err)
	}

	if _, err := s.messages.SetReaction(s.ctx, person("olga"), chat.ID, post.ID, "👍"); err != nil {
		t.Fatalf("the same kind: %v", err)
	}

	if _, err := s.messages.SetReaction(s.ctx, person("petro"), chat.ID, post.ID, "🔥"); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a swap to a second kind: %v", err)
	}

	none := domain.ChatRules{ReactionsMode: domain.ReactionsNone}
	if _, err := s.messages.SetRules(s.ctx, person("owner"), chat.ID, none); err != nil {
		t.Fatal(err)
	}

	if _, err := s.messages.SetReaction(s.ctx, person("olga"), chat.ID, post.ID, "👍"); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("reactions are off: %v", err)
	}

	if _, err := s.messages.SetReaction(s.ctx, person("olga"), chat.ID, post.ID, ""); err != nil {
		t.Fatalf("taking a reaction back: %v", err)
	}

	empty := domain.ChatRules{ReactionsMode: domain.ReactionsSome}
	if _, err := s.messages.SetRules(s.ctx, person("owner"), chat.ID, empty); !errors.Is(err, domain.ErrInvalidReaction) {
		t.Fatalf("an empty list: %v", err)
	}

	chats, err := s.messages.Chats(s.ctx, person("petro"), 0)
	if err != nil || len(chats) == 0 || chats[0].Rules.ReactionsMode != domain.ReactionsNone || chats[0].Rules.NoCopy {
		t.Fatalf("got %+v, %v", chats, err)
	}
}
