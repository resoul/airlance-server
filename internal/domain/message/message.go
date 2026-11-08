package message

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ScopeRead  = "messages:read"
	ScopeWrite = "messages:write"

	MaxTextLength     = 4096
	MaxClientIDLength = 64
	MaxHistoryPage    = 100
	MaxChatsPage      = 100
)

var (
	ErrInvalidText     = errors.New("invalid message text")
	ErrInvalidClientID = errors.New("invalid client id")
	ErrInvalidPeer     = errors.New("invalid peer")
	ErrPeerNotFound    = errors.New("peer not found")
	ErrBlocked         = errors.New("blocked")
	ErrRestricted      = errors.New("restricted by privacy settings")
	ErrChatNotFound    = errors.New("chat not found")
)

type Message struct {
	ChatID      string
	ID          int64
	Sender      string
	ClientID    string
	Text        string
	SentAt      time.Time
	ChatKind    Kind
	Author      Author
	AgentID     string
	AgentName   string
	ForwardName string
	Attachments []Attachment
	ReplyToID   int64
	Entities    []Entity
	EditedAt    time.Time
	IsDeleted   bool
	ThreadID    int64
	Reactions   []ReactionCount
	MyReaction  string
	Poll        *Poll
}

type Delivery struct {
	Owner     string
	ChatID    string
	ClientID  string
	Text      string
	AgentID   string
	AgentName string
}

type Author struct {
	FirstName    string
	LastName     string
	PhotoMediaID string
	VideoMediaID string
}

type Peer struct {
	Subject      string
	FirstName    string
	LastName     string
	Username     string
	PhotoMediaID string
	VideoMediaID string
}

type Chat struct {
	ID          string
	Peer        Peer
	Last        *Message
	Kind        Kind
	Title       string
	About       string
	Link        string
	InviteToken string
	MemberCount int
	Role        Role
	IsMember    bool
	IsPinned    bool
	IsMuted     bool
	MyReadID    int64
	PeerReadID  int64
	Unread      int32
	IsArchived  bool
	Draft       string
	DraftAtMs   int64

	AdminRights  Rights
	MemberRights Rights
	Rules        ChatRules

	PeerDeliveredID int64
}

func (m Message) ForViewer(viewer string) Message {
	if m.ChatKind != KindChannel || m.Sender == viewer {
		return m
	}

	m.Sender = m.ChatID
	m.ClientID = strconv.FormatInt(m.ID, 10)
	m.Author = Author{}

	return m
}

func New(chatID string, id int64, sender, clientID, text string, now time.Time) (Message, error) {
	clean := strings.TrimSpace(text)
	if clean == "" || !utf8.ValidString(clean) || utf8.RuneCountInString(clean) > MaxTextLength {
		return Message{}, ErrInvalidText
	}

	if clientID == "" || len(clientID) > MaxClientIDLength || !utf8.ValidString(clientID) {
		return Message{}, ErrInvalidClientID
	}

	return Message{ChatID: chatID, ID: id, Sender: sender, ClientID: clientID, Text: clean, SentAt: now}, nil
}

func DirectKey(first, second string) string {
	if first > second {
		first, second = second, first
	}

	sum := sha256.Sum256([]byte(first + "\x00" + second))

	return hex.EncodeToString(sum[:])
}

func Revise(text string, attachments int, entities []Entity) (string, error) {
	clean := strings.TrimSpace(text)
	if !utf8.ValidString(clean) || utf8.RuneCountInString(clean) > MaxTextLength {
		return "", ErrInvalidText
	}

	if clean == "" && attachments == 0 {
		return "", ErrInvalidText
	}

	if err := checkEntities(entities); err != nil {
		return "", err
	}

	return clean, nil
}

func (m Message) MayEdit(subject string, now time.Time) error {
	if m.IsDeleted || m.Sender != subject || m.AgentID != "" || m.ForwardName != "" || m.Poll != nil {
		return ErrNothingToEdit
	}

	if now.Sub(m.SentAt) > EditWindow {
		return ErrEditExpired
	}

	return nil
}

func MayDeleteForAll(access Access, subject, sender string) bool {
	return access.Kind == KindDirect || sender == subject || access.Can(RightDelete)
}
