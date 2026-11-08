package message_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"go.airlance.org/server/internal/domain/message"
)

func TestRevise(t *testing.T) {
	t.Parallel()

	bold := []message.Entity{{Kind: message.EntityBold, Offset: 0, Length: 2}}

	if got, err := message.Revise("  hi  ", 0, bold); err != nil || got != "hi" {
		t.Fatalf("got %q, %v", got, err)
	}

	if _, err := message.Revise("   ", 0, nil); !errors.Is(err, message.ErrInvalidText) {
		t.Fatalf("an empty text without files: %v", err)
	}

	if got, err := message.Revise("", 1, nil); err != nil || got != "" {
		t.Fatalf("a caption may be removed: %q, %v", got, err)
	}

	broken := []message.Entity{{Kind: message.EntityMention, Offset: 0, Length: 2}}
	if _, err := message.Revise("hi", 0, broken); !errors.Is(err, message.ErrInvalidEntity) {
		t.Fatalf("a mention with no person: %v", err)
	}
}

func TestOnlyTheAuthorEditsInTime(t *testing.T) {
	t.Parallel()

	sent := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	own := message.Message{Sender: "anna", SentAt: sent}

	if err := own.MayEdit("anna", sent.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if err := own.MayEdit("petro", sent.Add(time.Hour)); !errors.Is(err, message.ErrNothingToEdit) {
		t.Fatalf("another person edited: %v", err)
	}

	if err := own.MayEdit("anna", sent.Add(message.EditWindow+time.Minute)); !errors.Is(err, message.ErrEditExpired) {
		t.Fatalf("an old message was edited: %v", err)
	}

	forwarded := message.Message{Sender: "anna", SentAt: sent, ForwardName: "Олена"}
	if err := forwarded.MayEdit("anna", sent); !errors.Is(err, message.ErrNothingToEdit) {
		t.Fatalf("a forward was edited: %v", err)
	}
}

func TestWhoMayDeleteForEveryone(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		kind message.Kind
		role message.Role
		who  string
		want bool
	}{
		{"either side of a direct chat", message.KindDirect, message.RoleMember, "petro", true},
		{"the author in a group", message.KindGroup, message.RoleMember, "anna", true},
		{"a member in a group", message.KindGroup, message.RoleMember, "petro", false},
		{"an admin in a group", message.KindGroup, message.RoleAdmin, "petro", true},
		{"a subscriber of a channel", message.KindChannel, message.RoleMember, "petro", false},
		{"the owner of a channel", message.KindChannel, message.RoleOwner, "petro", true},
	}

	for _, next := range cases {
		if got := message.MayDeleteForAll(message.Access{Kind: next.kind, Role: next.role, Admin: message.DefaultAdminRights}, next.who, "anna"); got != next.want {
			t.Errorf("%s: got %v", next.name, got)
		}
	}
}

func TestComposeKeepsReplyAndEntities(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	outgoing := message.Outgoing{
		Text: "hi", ReplyToID: 5, Entities: []message.Entity{{Kind: message.EntityItalic, Offset: 0, Length: 2}},
	}

	next, err := message.Compose("c", 9, "anna", "local", outgoing, now)
	if err != nil || next.ReplyToID != 5 || len(next.Entities) != 1 {
		t.Fatalf("got %+v, %v", next, err)
	}

	outgoing.ReplyToID = -1
	if _, err := message.Compose("c", 9, "anna", "local", outgoing, now); !errors.Is(err, message.ErrInvalidReply) {
		t.Fatalf("a negative reply: %v", err)
	}

	outgoing.ReplyToID = 0
	outgoing.Entities = []message.Entity{{Kind: 99, Offset: 0, Length: 1}}

	if _, err := message.Compose("c", 9, "anna", "local", outgoing, now); !errors.Is(err, message.ErrInvalidEntity) {
		t.Fatalf("an unknown entity: %v", err)
	}
}

func TestWhoMayPin(t *testing.T) {
	t.Parallel()

	direct := message.Access{Kind: message.KindDirect}
	member := message.Access{Kind: message.KindGroup, Member: message.DefaultMemberRights}
	admin := message.Access{Kind: message.KindChannel, Role: message.RoleAdmin, Admin: message.DefaultAdminRights}

	if !message.MayPin(direct) || message.MayPin(member) {
		t.Fatal("the pin rule is wrong for members")
	}

	if !message.MayPin(admin) {
		t.Fatal("an admin may pin")
	}

	member.Member |= message.RightPin
	if !message.MayPin(member) {
		t.Fatal("a member with the right may pin")
	}
}

func TestCheckReaction(t *testing.T) {
	t.Parallel()

	for _, good := range []string{"", "👍", "❤️", "🏳️‍🌈"} {
		if err := message.CheckReaction(good); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}

	for _, bad := range []string{"a b", "\n", "\xff", "0123456789012345678901234567890123456789"} {
		if err := message.CheckReaction(bad); !errors.Is(err, message.ErrInvalidReaction) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestLocationAndContactCards(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	compose := func(next message.Attachment) error {
		_, err := message.Compose("c", 1, "anna", "l", message.Outgoing{Attachments: []message.Attachment{next}}, now)

		return err
	}

	place := message.Attachment{Kind: message.AttachmentLocation, Latitude: 50.45, Longitude: 30.52, Title: "Київ"}
	if err := compose(place); err != nil {
		t.Fatalf("a place: %v", err)
	}

	place.Latitude = 91
	if err := compose(place); !errors.Is(err, message.ErrInvalidAttachment) {
		t.Fatalf("a place off the globe: %v", err)
	}

	place.Latitude, place.MediaID = 10, "file-1"
	if err := compose(place); !errors.Is(err, message.ErrInvalidAttachment) {
		t.Fatalf("a place with a file: %v", err)
	}

	card := message.Attachment{Kind: message.AttachmentContact, Name: "Олена Коваль", Phone: "+380501112233"}
	if err := compose(card); err != nil {
		t.Fatalf("a contact: %v", err)
	}

	card.Phone = ""
	if err := compose(card); !errors.Is(err, message.ErrInvalidAttachment) {
		t.Fatalf("a contact with no phone: %v", err)
	}

	card.Phone, card.Name = "+380501112233", "  "
	if err := compose(card); !errors.Is(err, message.ErrInvalidAttachment) {
		t.Fatalf("a contact with no name: %v", err)
	}

	next, err := message.Compose("c", 1, "anna", "l", message.Outgoing{Attachments: []message.Attachment{place}}, now)
	if err == nil && len(next.MediaIDs()) != 1 {
		t.Fatal("a card has no file")
	}
}

func TestPollRules(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	draft := func() message.Poll {
		return message.Poll{
			Question: " Куди? ", Options: []message.PollOption{{Text: "Ліс"}, {Text: "Море"}},
		}
	}

	good, err := message.NewPoll(draft(), now)
	if err != nil || good.Question != "Куди?" || good.Correct != -1 {
		t.Fatalf("got %+v, %v", good, err)
	}

	for name, change := range map[string]func(*message.Poll){
		"one option":       func(p *message.Poll) { p.Options = p.Options[:1] },
		"same options":     func(p *message.Poll) { p.Options[1].Text = "Ліс" },
		"empty option":     func(p *message.Poll) { p.Options[0].Text = " " },
		"no question":      func(p *message.Poll) { p.Question = "" },
		"quiz no answer":   func(p *message.Poll) { p.Quiz, p.Correct = true, 5 },
		"multi quiz":       func(p *message.Poll) { p.Quiz, p.Multiple = true, true },
		"past deadline":    func(p *message.Poll) { p.CloseAt = now.Add(-time.Minute) },
		"far deadline":     func(p *message.Poll) { p.CloseAt = now.Add(message.MaxPollDuration + time.Hour) },
		"why without quiz": func(p *message.Poll) { p.Explanation = "потому" },
	} {
		next := draft()
		change(&next)

		if _, err := message.NewPoll(next, now); !errors.Is(err, message.ErrInvalidPoll) {
			t.Errorf("%s: %v", name, err)
		}
	}

	if err := good.CheckVote([]int32{0, 1}); !errors.Is(err, message.ErrInvalidVote) {
		t.Errorf("two choices in a single poll: %v", err)
	}

	if err := good.CheckVote([]int32{2}); !errors.Is(err, message.ErrInvalidVote) {
		t.Errorf("an option that is not there: %v", err)
	}

	if err := good.CheckVote(nil); err != nil {
		t.Errorf("retracting: %v", err)
	}

	quiz := draft()
	quiz.Quiz, quiz.Correct = true, 1

	made, err := message.NewPoll(quiz, now)
	if err != nil || made.Correct != 1 || made.CheckVote(nil) == nil {
		t.Fatalf("got %+v, %v", made, err)
	}
}

func TestDraftText(t *testing.T) {
	t.Parallel()

	if got, err := message.CheckDraft("  привіт  "); err != nil || got != "  привіт  " {
		t.Fatalf("a draft keeps its spaces: %q, %v", got, err)
	}

	if got, err := message.CheckDraft(" \n "); err != nil || got != "" {
		t.Fatalf("a blank draft is no draft: %q, %v", got, err)
	}

	if _, err := message.CheckDraft(strings.Repeat("я", message.MaxTextLength+1)); !errors.Is(err, message.ErrInvalidText) {
		t.Fatalf("a long draft: %v", err)
	}
}
