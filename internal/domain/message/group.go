package message

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type Kind int16

const (
	KindDirect  Kind = 0
	KindGroup   Kind = 1
	KindChannel Kind = 2
)

type Role int16

const (
	RoleMember Role = 0
	RoleAdmin  Role = 1
	RoleOwner  Role = 2
)

const (
	MaxTitleLength   = 128
	MaxAboutLength   = 255
	MaxInitialPeople = 200
	MaxMembersPage   = 200
	MaxPinnedChats   = 20
)

var (
	ErrInvalidTitle  = errors.New("invalid chat title")
	ErrInvalidAbout  = errors.New("invalid chat description")
	ErrInvalidLink   = errors.New("invalid chat link")
	ErrInvalidKind   = errors.New("invalid chat kind")
	ErrLinkTaken     = errors.New("chat link is taken")
	ErrNotAllowed    = errors.New("not allowed in this chat")
	ErrTooManyPinned = errors.New("too many pinned chats")
)

var linkPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{4,31}$`)

type NewChat struct {
	ID          string
	CreatedBy   string
	ClientID    string
	Kind        Kind
	Title       string
	About       string
	Link        string
	InviteToken string
	CreatedAt   time.Time
}

type Member struct {
	Peer   Peer
	Role   Role
	Rights Rights
}

func (n NewChat) Checked() (NewChat, error) {
	if n.Kind != KindGroup && n.Kind != KindChannel {
		return NewChat{}, ErrInvalidKind
	}

	if n.ClientID == "" || len(n.ClientID) > MaxClientIDLength || !utf8.ValidString(n.ClientID) {
		return NewChat{}, ErrInvalidClientID
	}

	info, err := ChatInfo{Title: n.Title, About: n.About, Link: n.Link}.Checked()
	if err != nil {
		return NewChat{}, err
	}

	n.Title, n.About, n.Link = info.Title, info.About, info.Link

	return n, nil
}

type ChatInfo struct {
	Title string
	About string
	Link  string
}

func (i ChatInfo) Checked() (ChatInfo, error) {
	i.Title = strings.TrimSpace(i.Title)
	if i.Title == "" || !utf8.ValidString(i.Title) || utf8.RuneCountInString(i.Title) > MaxTitleLength {
		return ChatInfo{}, ErrInvalidTitle
	}

	i.About = strings.TrimSpace(i.About)
	if !utf8.ValidString(i.About) || utf8.RuneCountInString(i.About) > MaxAboutLength {
		return ChatInfo{}, ErrInvalidAbout
	}

	i.Link = strings.ToLower(strings.TrimSpace(i.Link))
	if i.Link != "" && !linkPattern.MatchString(i.Link) {
		return ChatInfo{}, ErrInvalidLink
	}

	return i, nil
}

func (r Role) Manages(other Role) bool {
	return r >= RoleAdmin && r > other
}
