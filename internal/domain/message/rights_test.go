package message_test

import (
	"testing"

	"go.airlance.org/server/internal/domain/message"
)

func TestAccessRules(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		access message.Access
		right  message.Rights
		want   bool
	}{
		{"a group member writes", message.Access{Kind: message.KindGroup, Member: message.DefaultMemberRights}, message.RightSend, true},
		{"a muted group member", message.Access{Kind: message.KindGroup}, message.RightSend, false},
		{"a member never deletes", message.Access{Kind: message.KindGroup, Member: message.AllRights}, message.RightDelete, false},
		{"a channel member never writes", message.Access{Kind: message.KindChannel, Member: message.AllRights}, message.RightSend, false},
		{"an admin always writes", message.Access{Kind: message.KindGroup, Role: message.RoleAdmin}, message.RightMedia, true},
		{"an admin without the right", message.Access{Kind: message.KindGroup, Role: message.RoleAdmin}, message.RightBan, false},
		{"an admin with the right", message.Access{Kind: message.KindGroup, Role: message.RoleAdmin, Admin: message.RightBan}, message.RightBan, true},
		{"the owner does everything", message.Access{Kind: message.KindChannel, Role: message.RoleOwner}, message.RightPromote, true},
		{"direct chats allow only chatting", message.Access{Kind: message.KindDirect}, message.RightDelete, false},
	}

	for _, next := range cases {
		if got := next.access.Can(next.right); got != next.want {
			t.Errorf("%s: got %v", next.name, got)
		}
	}
}

func TestAdminsGrantOnlyWhatTheyHave(t *testing.T) {
	t.Parallel()

	admin := message.Access{Kind: message.KindGroup, Role: message.RoleAdmin, Admin: message.RightBan | message.RightPromote}
	if !admin.CanGrant(message.RightBan) || admin.CanGrant(message.RightDelete) {
		t.Fatal("an admin granted a right they lack")
	}

	owner := message.Access{Kind: message.KindGroup, Role: message.RoleOwner}
	if !owner.CanGrant(message.AllRights) {
		t.Fatal("the owner grants anything")
	}

	if message.CleanAdminRights(message.RightSend|message.RightBan) != message.RightBan {
		t.Fatal("admin rights keep only admin bits")
	}
}
