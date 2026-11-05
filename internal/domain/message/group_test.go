package message_test

import (
	"errors"
	"testing"

	"go.airlance.org/server/internal/domain/message"
)

func TestANewChatIsChecked(t *testing.T) {
	t.Parallel()

	valid := message.NewChat{ClientID: "c", Kind: message.KindGroup, Title: " Семья ", Link: "Family_Chat"}

	checked, err := valid.Checked()
	if err != nil || checked.Title != "Семья" || checked.Link != "family_chat" {
		t.Fatalf("got %+v, %v", checked, err)
	}

	cases := map[string]struct {
		change func(*message.NewChat)
		want   error
	}{
		"a direct chat":   {func(n *message.NewChat) { n.Kind = message.KindDirect }, message.ErrInvalidKind},
		"no client id":    {func(n *message.NewChat) { n.ClientID = "" }, message.ErrInvalidClientID},
		"no title":        {func(n *message.NewChat) { n.Title = "  " }, message.ErrInvalidTitle},
		"a short link":    {func(n *message.NewChat) { n.Link = "abc" }, message.ErrInvalidLink},
		"a link with dot": {func(n *message.NewChat) { n.Link = "some.link" }, message.ErrInvalidLink},
	}

	for name, next := range cases {
		draft := valid
		next.change(&draft)

		if _, err := draft.Checked(); !errors.Is(err, next.want) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestAChannelHidesItsAuthorFromOthers(t *testing.T) {
	t.Parallel()

	post := message.Message{
		ChatID: "c", ID: 7, Sender: "admin", ClientID: "local", ChatKind: message.KindChannel,
		Author: message.Author{FirstName: "Анна"},
	}

	seen := post.ForViewer("reader")
	if seen.Sender != "c" || seen.ClientID != "7" || seen.Author.FirstName != "" {
		t.Fatalf("got %+v", seen)
	}

	if own := post.ForViewer("admin"); own.Sender != "admin" || own.ClientID != "local" {
		t.Fatalf("got %+v", own)
	}

	post.ChatKind = message.KindGroup
	if group := post.ForViewer("reader"); group.Sender != "admin" || group.Author.FirstName != "Анна" {
		t.Fatalf("got %+v", group)
	}
}

func TestRolesManageOnlyLowerOnes(t *testing.T) {
	t.Parallel()

	if !message.RoleOwner.Manages(message.RoleAdmin) || !message.RoleAdmin.Manages(message.RoleMember) {
		t.Fatal("a higher role must manage a lower one")
	}

	if message.RoleAdmin.Manages(message.RoleAdmin) || message.RoleMember.Manages(message.RoleMember) {
		t.Fatal("an equal role must not manage")
	}
}
