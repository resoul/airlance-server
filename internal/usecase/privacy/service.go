package privacy

import (
	"context"
	"fmt"
	"time"

	"go.airlance.org/server/internal/domain/directory"
	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/message"
	domain "go.airlance.org/server/internal/domain/privacy"
	"go.airlance.org/server/internal/domain/profile"
)

type Repository interface {
	Settings(ctx context.Context, subject string) (domain.Settings, error)
	SaveSettings(ctx context.Context, subject string, settings domain.Settings) error
	Relation(ctx context.Context, viewer, target string) (domain.Relation, error)
	SetBlocked(ctx context.Context, owner, target string, blocked bool, now time.Time) error
	Blocked(ctx context.Context, owner string) ([]directory.Person, error)
	BlockersOf(ctx context.Context, subject string) ([]string, error)
}

type Notifier interface {
	Notify(subject string)
}

type Service struct {
	repository Repository
	notifier   Notifier
	now        func() time.Time
}

func New(repository Repository, notifier Notifier, now func() time.Time) *Service {
	return &Service{repository: repository, notifier: notifier, now: now}
}

func (s *Service) Get(ctx context.Context, principal identity.Principal) (domain.Settings, error) {
	if !principal.HasScope(profile.ScopeRead) {
		return domain.Settings{}, identity.ErrForbidden
	}

	settings, err := s.repository.Settings(ctx, principal.Subject)
	if err != nil {
		return domain.Settings{}, fmt.Errorf("get privacy: %w", err)
	}

	return settings, nil
}

func (s *Service) Set(
	ctx context.Context,
	principal identity.Principal,
	settings domain.Settings,
) (domain.Settings, error) {
	if !principal.HasScope(profile.ScopeWrite) {
		return domain.Settings{}, identity.ErrForbidden
	}

	if err := settings.Validate(); err != nil {
		return domain.Settings{}, fmt.Errorf("check privacy: %w", err)
	}

	if err := s.repository.SaveSettings(ctx, principal.Subject, settings); err != nil {
		return domain.Settings{}, fmt.Errorf("save privacy: %w", err)
	}

	s.notifier.Notify(principal.Subject)

	return settings, nil
}

func (s *Service) Blocked(ctx context.Context, principal identity.Principal) ([]directory.Person, error) {
	if !principal.HasScope(profile.ScopeRead) {
		return nil, identity.ErrForbidden
	}

	people, err := s.repository.Blocked(ctx, principal.Subject)
	if err != nil {
		return nil, fmt.Errorf("read blocked: %w", err)
	}

	return people, nil
}

func (s *Service) SetBlocked(
	ctx context.Context,
	principal identity.Principal,
	target string,
	blocked bool,
) ([]directory.Person, error) {
	if !principal.HasScope(profile.ScopeWrite) {
		return nil, identity.ErrForbidden
	}

	if target == "" || target == principal.Subject {
		return nil, domain.ErrInvalidPeer
	}

	if err := s.repository.SetBlocked(ctx, principal.Subject, target, blocked, s.now()); err != nil {
		return nil, fmt.Errorf("set blocked: %w", err)
	}

	s.notifier.Notify(principal.Subject)
	s.notifier.Notify(target)

	return s.Blocked(ctx, principal)
}

func (s *Service) Filter(ctx context.Context, viewer string, target profile.Profile) (profile.Profile, error) {
	if viewer == target.Subject {
		return target, nil
	}

	relation, err := s.repository.Relation(ctx, viewer, target.Subject)
	if err != nil {
		return profile.Profile{}, fmt.Errorf("read relation: %w", err)
	}

	if relation.BlockedByTarget {
		return profile.Profile{}, profile.ErrNotFound
	}

	settings, err := s.repository.Settings(ctx, target.Subject)
	if err != nil {
		return profile.Profile{}, fmt.Errorf("read privacy: %w", err)
	}

	if !settings.Phone.Allows(relation) {
		target.Fields.Phone = ""
	}

	if !settings.Bio.Allows(relation) {
		target.Fields.Bio = ""
	}

	if !settings.Photos.Allows(relation) {
		target.Fields.PhotoMediaID = ""
		target.Fields.VideoMediaID = ""
		target.Photos = nil
	}

	return target, nil
}

func (s *Service) CheckOpen(ctx context.Context, from, to string) error {
	relation, err := s.repository.Relation(ctx, from, to)
	if err != nil {
		return fmt.Errorf("read relation: %w", err)
	}

	if relation.BlockedByTarget {
		return message.ErrPeerNotFound
	}

	if relation.BlocksTarget {
		return message.ErrBlocked
	}

	settings, err := s.repository.Settings(ctx, to)
	if err != nil {
		return fmt.Errorf("read privacy: %w", err)
	}

	if !settings.Messages.Allows(relation) && !relation.SharesChat {
		return message.ErrRestricted
	}

	return nil
}

func (s *Service) BlockersOf(ctx context.Context, subject string) ([]string, error) {
	blockers, err := s.repository.BlockersOf(ctx, subject)
	if err != nil {
		return nil, fmt.Errorf("read blockers: %w", err)
	}

	return blockers, nil
}
