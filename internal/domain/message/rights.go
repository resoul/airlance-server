package message

type Rights int64

const (
	RightSend    Rights = 1 << 0
	RightMedia   Rights = 1 << 1
	RightInvite  Rights = 1 << 2
	RightPin     Rights = 1 << 3
	RightInfo    Rights = 1 << 4
	RightDelete  Rights = 1 << 5
	RightBan     Rights = 1 << 6
	RightPromote Rights = 1 << 7

	AllRights Rights = 1<<8 - 1

	MemberBits          = RightSend | RightMedia | RightInvite | RightPin | RightInfo
	AdminBits           = RightInvite | RightPin | RightInfo | RightDelete | RightBan | RightPromote
	DefaultMemberRights = RightSend | RightMedia | RightInvite
	DefaultAdminRights  = RightInvite | RightPin | RightInfo | RightDelete | RightBan
)

type Access struct {
	Kind   Kind
	Role   Role
	Admin  Rights
	Member Rights
}

func (a Access) Can(right Rights) bool {
	switch {
	case a.Kind == KindDirect:
		return right&(RightSend|RightMedia|RightPin) == right
	case a.Role == RoleOwner:
		return true
	case a.Role == RoleAdmin:
		return right&(RightSend|RightMedia) == right || a.Admin&right == right
	case a.Kind == KindChannel:
		return false
	default:
		return a.Member&MemberBits&right == right
	}
}

func (a Access) Mask() Rights {
	var mask Rights

	for bit := Rights(1); bit <= RightPromote; bit <<= 1 {
		if a.Can(bit) {
			mask |= bit
		}
	}

	return mask
}

func (a Access) CanGrant(rights Rights) bool {
	return a.Role == RoleOwner || a.Mask()&rights == rights
}

func (c Chat) MyRights() Rights {
	if !c.IsMember {
		return 0
	}

	return Access{Kind: c.Kind, Role: c.Role, Admin: c.AdminRights, Member: c.MemberRights}.Mask()
}

func CleanAdminRights(rights Rights) Rights {
	return rights & AdminBits
}

func CleanMemberRights(rights Rights) Rights {
	return rights & MemberBits
}
