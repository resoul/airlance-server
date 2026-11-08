package seed

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math/rand/v2"
	"sync"
	"time"

	"go.airlance.org/server/internal/domain/contact"
	"go.airlance.org/server/internal/domain/directory"
	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/media"
	"go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/privacy"
	"go.airlance.org/server/internal/domain/profile"
	usecase "go.airlance.org/server/internal/usecase/message"
)

const (
	subjectPrefix = "seed:"
	linkHost      = "link.airlance.org"
	avatarSide    = 320
	avatarQuality = 85
	historySpan   = 72 * time.Hour
	liveEvery     = 4 * time.Second
	liveLimit     = 10 * time.Minute
)

var ErrPersonNotFound = errors.New("no account with this username")

type Messaging interface {
	Create(ctx context.Context, principal identity.Principal, next usecase.NewChat) (message.Chat, error)
	Join(ctx context.Context, principal identity.Principal, code string) (message.Chat, error)
	OpenDirect(ctx context.Context, principal identity.Principal, peer string) (message.Chat, error)
	AddMember(
		ctx context.Context,
		principal identity.Principal,
		chatID, subject string,
		role message.Role,
		rights message.Rights,
	) ([]message.Member, error)
	Send(ctx context.Context, principal identity.Principal, chatID, clientID, text string) (message.Message, error)
	Edit(
		ctx context.Context,
		principal identity.Principal,
		chatID string,
		id int64,
		text string,
		entities []message.Entity,
	) (message.Message, error)
	SetPin(ctx context.Context, principal identity.Principal, chatID string, id int64, isPinned bool) ([]int64, error)
	SetReaction(
		ctx context.Context,
		principal identity.Principal,
		chatID string,
		id int64,
		emoji string,
	) (message.Message, error)
	Vote(
		ctx context.Context,
		principal identity.Principal,
		chatID string,
		id int64,
		options []int32,
	) (message.Message, error)
	SetArchived(ctx context.Context, principal identity.Principal, chatID string, isArchived bool) (message.Chat, error)
	SetDraft(ctx context.Context, principal identity.Principal, chatID, text string) (message.DraftState, error)
	SendContent(
		ctx context.Context,
		principal identity.Principal,
		chatID, clientID string,
		outgoing message.Outgoing,
	) (message.Message, error)
}

type Profiles interface {
	GetOwn(ctx context.Context, principal identity.Principal) (profile.Profile, error)
	UpdateOwn(ctx context.Context, principal identity.Principal, fields profile.Fields) (profile.Profile, error)
	AddPhoto(ctx context.Context, principal identity.Principal, photoMediaID, videoMediaID string) (profile.Profile, error)
}

type Contacts interface {
	Add(ctx context.Context, principal identity.Principal, subject, first, last string) ([]contact.Contact, error)
}

type Privacy interface {
	Set(ctx context.Context, principal identity.Principal, settings privacy.Settings) (privacy.Settings, error)
}

type Media interface {
	Create(ctx context.Context, object media.Object) error
	Get(ctx context.Context, id string) (media.Object, error)
}

type Files interface {
	Put(ctx context.Context, id string, data []byte) error
	Remove(ctx context.Context, id string) error
}

type Index interface {
	Upsert(ctx context.Context, people []directory.Person) error
}

type Store interface {
	SubjectOfUsername(ctx context.Context, username string) (string, error)
	Purge(ctx context.Context, prefix string, now time.Time) (Purged, error)
}

type Purged struct {
	Subjects []string
	MediaIDs []string
	Woken    []string
}

type Notifier interface {
	Notify(subject string)
}

type Clock struct {
	mu   sync.Mutex
	time time.Time
}

func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.time.IsZero() {
		return time.Now()
	}

	return c.time
}

func (c *Clock) Set(next time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.time = next
}

type Deps struct {
	Messaging Messaging
	Profiles  Profiles
	Contacts  Contacts
	Privacy   Privacy
	Media     Media
	Files     Files
	Index     Index
	Store     Store
	Notifier  Notifier
	Clock     *Clock
	Print     func(string)
}

type Service struct {
	Deps
}

func New(deps Deps) *Service {
	return &Service{Deps: deps}
}

func principalOf(subject string) identity.Principal {
	return identity.Principal{Subject: subject, Scopes: []string{
		profile.ScopeRead, profile.ScopeWrite, message.ScopeRead, message.ScopeWrite,
	}}
}

func subjectOf(index int) string {
	return subjectPrefix + people[index].key
}

func usernameOf(index int) string {
	return "seed_" + people[index].key
}

func (s *Service) Run(ctx context.Context, username string) error {
	viewer, err := s.findViewer(ctx, username)
	if err != nil {
		return err
	}

	if err := s.createPeople(ctx); err != nil {
		return err
	}

	s.Clock.Set(time.Now().Add(-historySpan))

	chats, err := s.createRooms(ctx, viewer)
	if err != nil {
		return err
	}

	if err := s.writeHistory(ctx, chats); err != nil {
		return err
	}

	if viewer != "" {
		if err := s.greetViewer(ctx, viewer); err != nil {
			return err
		}
	}

	if err := s.writeRichMessages(ctx, chats, username); err != nil {
		return err
	}

	if err := s.writeFeatures(ctx, chats, viewer); err != nil {
		return err
	}

	s.Clock.Set(time.Time{})

	if err := s.applyPrivacy(ctx); err != nil {
		return err
	}

	s.Print(fmt.Sprintf("Готово: %d человек, %d чатов", len(people), len(rooms)))

	return nil
}

func (s *Service) findViewer(ctx context.Context, username string) (string, error) {
	if username == "" {
		return "", nil
	}

	viewer, err := s.Store.SubjectOfUsername(ctx, username)
	if err != nil {
		return "", fmt.Errorf("find @%s: %w", username, err)
	}

	return viewer, nil
}

func (s *Service) createPeople(ctx context.Context) error {
	for index, next := range people {
		principal := principalOf(subjectOf(index))

		_, err := s.Profiles.UpdateOwn(ctx, principal, profile.Fields{
			FirstName: next.firstName,
			LastName:  next.lastName,
			Username:  usernameOf(index),
			Bio:       next.bio,
			Phone:     fmt.Sprintf("+1555010%02d", index),
		})
		if err != nil {
			return fmt.Errorf("create %s: %w", next.key, err)
		}

		if err := s.ensurePhoto(ctx, principal, next); err != nil {
			return fmt.Errorf("photo of %s: %w", next.key, err)
		}
	}

	return nil
}

func (s *Service) ensurePhoto(ctx context.Context, principal identity.Principal, next person) error {
	own, err := s.Profiles.GetOwn(ctx, principal)
	if err != nil {
		return fmt.Errorf("read profile: %w", err)
	}

	if own.Fields.PhotoMediaID != "" {
		return nil
	}

	data, err := avatar(next.color)
	if err != nil {
		return err
	}

	id := fmt.Sprintf("%x", []byte("seed-avatar-"+next.key))

	object, err := media.New(id, principal.Subject, int64(len(data)), time.Now())
	if err != nil {
		return fmt.Errorf("check media: %w", err)
	}

	if err := s.Files.Put(ctx, id, data); err != nil {
		return fmt.Errorf("store file: %w", err)
	}

	if err := s.Media.Create(ctx, object); err != nil {
		return fmt.Errorf("create media: %w", err)
	}

	if _, err := s.Profiles.AddPhoto(ctx, principal, id, ""); err != nil {
		return fmt.Errorf("add photo: %w", err)
	}

	return nil
}

func avatar(colors [2][3]uint8) ([]byte, error) {
	canvas := image.NewRGBA(image.Rect(0, 0, avatarSide, avatarSide))

	for y := range avatarSide {
		for x := range avatarSide {
			position := (x + y) * 255 / (2 * avatarSide)
			canvas.Set(x, y, color.RGBA{
				R: mix(colors[0][0], colors[1][0], position),
				G: mix(colors[0][1], colors[1][1], position),
				B: mix(colors[0][2], colors[1][2], position),
				A: 255,
			})
		}
	}

	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, canvas, &jpeg.Options{Quality: avatarQuality}); err != nil {
		return nil, fmt.Errorf("encode avatar: %w", err)
	}

	return encoded.Bytes(), nil
}

func mix(from, to uint8, position int) uint8 {
	return uint8((int(from)*(255-position) + int(to)*position) / 255)
}

func (s *Service) createRooms(ctx context.Context, viewer string) (map[string]string, error) {
	chats := make(map[string]string, len(rooms))

	for _, next := range rooms {
		invited := make([]string, len(next.members))
		for index, member := range next.members {
			invited[index] = subjectOf(member)
		}

		owner := principalOf(subjectOf(next.owner))

		chat, err := s.Messaging.Create(ctx, owner, usecase.NewChat{
			ClientID: "seed-" + next.key,
			Kind:     next.kind,
			Title:    next.title,
			About:    next.about,
			Link:     next.link,
			People:   invited,
		})
		if err != nil {
			return nil, fmt.Errorf("create %s: %w", next.key, err)
		}

		chats[next.key] = chat.ID
		s.printLink(next, chat)

		for _, admin := range rolesAdmin[next.key] {
			_, err := s.Messaging.AddMember(
				ctx, owner, chat.ID, subjectOf(admin), message.RoleAdmin, message.DefaultAdminRights,
			)
			if err != nil {
				return nil, fmt.Errorf("promote in %s: %w", next.key, err)
			}
		}

		if viewer != "" && next.key != "books" && next.key != "tech" {
			if err := s.joinViewer(ctx, viewer, chat); err != nil {
				return nil, fmt.Errorf("join %s: %w", next.key, err)
			}

			if viewerAdmin[next.key] {
				if _, err := s.Messaging.AddMember(
					ctx, owner, chat.ID, viewer, message.RoleAdmin, message.DefaultAdminRights,
				); err != nil {
					return nil, fmt.Errorf("promote the viewer in %s: %w", next.key, err)
				}
			}
		}
	}

	return chats, nil
}

func (s *Service) printLink(next room, chat message.Chat) {
	if chat.Link != "" {
		s.Print(next.title + ": https://" + linkHost + "/c/" + chat.Link)

		return
	}

	s.Print(next.title + ": https://" + linkHost + "/join/" + chat.InviteToken)
}

func (s *Service) joinViewer(ctx context.Context, viewer string, chat message.Chat) error {
	code := chat.Link
	if code == "" {
		code = chat.InviteToken
	}

	if _, err := s.Messaging.Join(ctx, principalOf(viewer), code); err != nil {
		return fmt.Errorf("join: %w", err)
	}

	return nil
}

func (s *Service) writeHistory(ctx context.Context, chats map[string]string) error {
	base := s.Clock.Now()

	for _, next := range rooms {
		gap := historySpan / time.Duration(len(next.lines)+1)
		for index, row := range next.lines {
			s.Clock.Set(base.Add(gap * time.Duration(index+1)))

			clientID := fmt.Sprintf("seed-%s-%d", next.key, index)

			_, err := s.Messaging.Send(ctx, principalOf(subjectOf(row.from)), chats[next.key], clientID, row.text)
			if err != nil {
				return fmt.Errorf("write %s: %w", next.key, err)
			}
		}
	}

	return nil
}

func (s *Service) greetViewer(ctx context.Context, viewer string) error {
	for index, next := range directs {
		principal := principalOf(subjectOf(next.from))

		chat, err := s.Messaging.OpenDirect(ctx, principal, viewer)
		if errors.Is(err, message.ErrRestricted) || errors.Is(err, message.ErrPeerNotFound) {
			s.Print("Пропускаю личный чат: приватность не пускает " + people[next.from].key)

			continue
		}

		if err != nil {
			return fmt.Errorf("open direct: %w", err)
		}

		s.Clock.Set(time.Now().Add(-time.Duration(len(directs)-index) * time.Hour))

		for number, text := range next.lines {
			clientID := fmt.Sprintf("seed-direct-%d-%d", index, number)
			if _, err := s.Messaging.Send(ctx, principal, chat.ID, clientID, text); err != nil {
				return fmt.Errorf("write direct: %w", err)
			}
		}

		_, err = s.Contacts.Add(ctx, principalOf(viewer), subjectOf(next.from), "", "")
		if err != nil {
			s.Print("Не добавил контакт " + people[next.from].key + ": " + err.Error())
		}
	}

	return nil
}

func (s *Service) applyPrivacy(ctx context.Context) error {
	settings := privacy.Settings{Phone: privacy.Everyone}

	hidden := settings
	hidden.Search = privacy.Nobody

	if _, err := s.Privacy.Set(ctx, principalOf(subjectOf(hiddenFromSearch)), hidden); err != nil {
		return fmt.Errorf("hide person: %w", err)
	}

	closed := settings
	closed.Messages = privacy.Contacts
	closed.Photos = privacy.Contacts

	if _, err := s.Privacy.Set(ctx, principalOf(subjectOf(contactsOnly)), closed); err != nil {
		return fmt.Errorf("close person: %w", err)
	}

	return nil
}

func (s *Service) Live(ctx context.Context) error {
	s.Clock.Set(time.Now().Add(-historySpan))

	chats, err := s.createRooms(ctx, "")
	if err != nil {
		return err
	}

	s.Clock.Set(time.Time{})

	ctx, cancel := context.WithTimeout(ctx, liveLimit)
	defer cancel()

	ticker := time.NewTicker(liveEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := s.liveMessage(ctx, chats); err != nil && ctx.Err() == nil {
				return err
			}
		}
	}
}

func (s *Service) liveMessage(ctx context.Context, chats map[string]string) error {
	next := liveRooms()[rand.IntN(len(liveRooms()))]
	lines := liveLines[next.key]
	text := lines[rand.IntN(len(lines))]

	sender := next.owner
	if next.kind == message.KindGroup && rand.IntN(4) > 0 {
		sender = next.members[rand.IntN(len(next.members))]
	}

	clientID := fmt.Sprintf("live-%d", time.Now().UnixNano())

	_, err := s.Messaging.Send(ctx, principalOf(subjectOf(sender)), chats[next.key], clientID, text)
	if err != nil {
		return fmt.Errorf("write live: %w", err)
	}

	s.Print(next.title + ": " + text)

	return nil
}

func liveRooms() []room {
	found := make([]room, 0, len(rooms))

	for _, next := range rooms {
		if len(liveLines[next.key]) > 0 {
			found = append(found, next)
		}
	}

	return found
}

func (s *Service) Clean(ctx context.Context) error {
	purged, err := s.Store.Purge(ctx, subjectPrefix, time.Now())
	if err != nil {
		return fmt.Errorf("purge: %w", err)
	}

	for _, id := range purged.MediaIDs {
		if err := s.Files.Remove(ctx, id); err != nil {
			s.Print("Не удалил файл " + id + ": " + err.Error())
		}
	}

	hidden := make([]directory.Person, len(purged.Subjects))
	for index, subject := range purged.Subjects {
		hidden[index] = directory.Person{Subject: subject, IsHidden: true}
	}

	if len(hidden) > 0 {
		if err := s.Index.Upsert(ctx, hidden); err != nil {
			s.Print("Поиск не обновлён: " + err.Error())
		}
	}

	for _, subject := range purged.Woken {
		s.Notifier.Notify(subject)
	}

	s.Print(fmt.Sprintf("Удалено: %d человек", len(purged.Subjects)))

	return nil
}
