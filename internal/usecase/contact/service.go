package contact

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	domain "go.airlance.org/server/internal/domain/contact"
	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/privacy"
	"go.airlance.org/server/internal/domain/profile"
)

const (
	lookupsPerWindow = 30
	lookupWindow     = time.Hour
)

type Added struct {
	Owner           string
	Subject         string
	CustomFirstName string
	CustomLastName  string
	AddedAt         time.Time
}

type Repository interface {
	Add(ctx context.Context, next Added) error
	Remove(ctx context.Context, owner, subject string) error
	List(ctx context.Context, owner string) ([]domain.Contact, error)
	FindByPhone(ctx context.Context, owner, digits string) ([]string, error)
	Sync(ctx context.Context, owner string, phones []string, now time.Time) (map[string]string, error)
}

type Relations interface {
	Relation(ctx context.Context, viewer, target string) (privacy.Relation, error)
}

type Profiles interface {
	Get(ctx context.Context, subject string) (profile.Profile, error)
}

type Notifier interface {
	Notify(subject string)
}

type lookups struct {
	start time.Time
	count int
}

type Service struct {
	repository Repository
	relations  Relations
	profiles   Profiles
	notifier   Notifier
	now        func() time.Time

	mu      sync.Mutex
	windows map[string]lookups
}

func New(
	repository Repository,
	relations Relations,
	profiles Profiles,
	notifier Notifier,
	now func() time.Time,
) *Service {
	return &Service{
		repository: repository,
		relations:  relations,
		profiles:   profiles,
		notifier:   notifier,
		now:        now,
		windows:    map[string]lookups{},
	}
}

func (s *Service) List(ctx context.Context, principal identity.Principal) ([]domain.Contact, error) {
	if !principal.HasScope(profile.ScopeRead) {
		return nil, identity.ErrForbidden
	}

	contacts, err := s.repository.List(ctx, principal.Subject)
	if err != nil {
		return nil, fmt.Errorf("list contacts: %w", err)
	}

	return contacts, nil
}

func (s *Service) Add(
	ctx context.Context,
	principal identity.Principal,
	subject, first, last string,
) ([]domain.Contact, error) {
	if !principal.HasScope(profile.ScopeWrite) {
		return nil, identity.ErrForbidden
	}

	if subject == "" || subject == principal.Subject {
		return nil, domain.ErrInvalidPeer
	}

	next, err := s.draft(principal.Subject, subject, first, last)
	if err != nil {
		return nil, err
	}

	if err := s.checkTarget(ctx, principal.Subject, subject); err != nil {
		return nil, err
	}

	if err := s.repository.Add(ctx, next); err != nil {
		return nil, fmt.Errorf("add contact: %w", err)
	}

	return s.changed(ctx, principal)
}

func (s *Service) AddByPhone(
	ctx context.Context,
	principal identity.Principal,
	phone, first, last string,
) ([]domain.Contact, error) {
	if !principal.HasScope(profile.ScopeWrite) {
		return nil, identity.ErrForbidden
	}

	digits, err := domain.NormalizePhone(phone)
	if err != nil {
		return nil, fmt.Errorf("check phone: %w", err)
	}

	if !s.allow(principal.Subject) {
		return nil, domain.ErrTooManyTries
	}

	found, err := s.repository.FindByPhone(ctx, principal.Subject, digits)
	if err != nil {
		return nil, fmt.Errorf("find by phone: %w", err)
	}

	if len(found) == 0 {
		return nil, domain.ErrNotFound
	}

	for _, subject := range found {
		next, err := s.draft(principal.Subject, subject, first, last)
		if err != nil {
			return nil, err
		}

		if err := s.repository.Add(ctx, next); err != nil {
			return nil, fmt.Errorf("add contact: %w", err)
		}
	}

	return s.changed(ctx, principal)
}

func (s *Service) Remove(ctx context.Context, principal identity.Principal, subject string) ([]domain.Contact, error) {
	if !principal.HasScope(profile.ScopeWrite) {
		return nil, identity.ErrForbidden
	}

	if err := s.repository.Remove(ctx, principal.Subject, subject); err != nil {
		return nil, fmt.Errorf("remove contact: %w", err)
	}

	return s.changed(ctx, principal)
}

func (s *Service) Sync(ctx context.Context, principal identity.Principal, raw []string) ([]domain.Contact, error) {
	if !principal.HasScope(profile.ScopeWrite) {
		return nil, identity.ErrForbidden
	}

	if len(raw) > domain.MaxSyncPhones {
		return nil, domain.ErrTooManyPhones
	}

	if !s.allow(principal.Subject) {
		return nil, domain.ErrTooManyTries
	}

	phones := make([]string, 0, len(raw))

	for _, next := range raw {
		if digits, err := domain.NormalizePhone(next); err == nil {
			phones = append(phones, digits)
		}
	}

	matched, err := s.repository.Sync(ctx, principal.Subject, phones, s.now())
	if err != nil {
		return nil, fmt.Errorf("sync contacts: %w", err)
	}

	contacts, err := s.changed(ctx, principal)
	if err != nil {
		return nil, err
	}

	for index := range contacts {
		if phone, ok := matched[contacts[index].Subject]; ok {
			contacts[index].Phone = phone
		}
	}

	return contacts, nil
}

func (s *Service) draft(owner, subject, first, last string) (Added, error) {
	cleanFirst, err := domain.CleanName(first)
	if err != nil {
		return Added{}, fmt.Errorf("check name: %w", err)
	}

	cleanLast, err := domain.CleanName(last)
	if err != nil {
		return Added{}, fmt.Errorf("check name: %w", err)
	}

	return Added{
		Owner:           owner,
		Subject:         subject,
		CustomFirstName: cleanFirst,
		CustomLastName:  cleanLast,
		AddedAt:         s.now(),
	}, nil
}

func (s *Service) checkTarget(ctx context.Context, owner, subject string) error {
	if _, err := s.profiles.Get(ctx, subject); errors.Is(err, profile.ErrNotFound) {
		return domain.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("get contact: %w", err)
	}

	relation, err := s.relations.Relation(ctx, owner, subject)
	if err != nil {
		return fmt.Errorf("read relation: %w", err)
	}

	if relation.BlockedByTarget {
		return domain.ErrNotFound
	}

	return nil
}

func (s *Service) changed(ctx context.Context, principal identity.Principal) ([]domain.Contact, error) {
	s.notifier.Notify(principal.Subject)

	contacts, err := s.repository.List(ctx, principal.Subject)
	if err != nil {
		return nil, fmt.Errorf("list contacts: %w", err)
	}

	return contacts, nil
}

func (s *Service) allow(subject string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	current := s.windows[subject]

	if now.Sub(current.start) >= lookupWindow {
		current = lookups{start: now}
	}

	if current.count >= lookupsPerWindow {
		return false
	}

	current.count++
	s.windows[subject] = current

	return true
}
