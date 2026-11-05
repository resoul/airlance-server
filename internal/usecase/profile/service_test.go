package profile_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/media"
	domain "go.airlance.org/server/internal/domain/profile"
	"go.airlance.org/server/internal/usecase/profile"
)

type memoryRepository struct {
	saved map[string]domain.Profile
}

func (m *memoryRepository) Get(_ context.Context, subject string) (domain.Profile, error) {
	got, ok := m.saved[subject]
	if !ok {
		return domain.Profile{}, domain.ErrNotFound
	}

	return got, nil
}

func (m *memoryRepository) Save(_ context.Context, next domain.Profile) error {
	m.saved[next.Subject] = next

	return nil
}

func (m *memoryRepository) AddPhoto(_ context.Context, subject string, photo domain.Photo) error {
	got, ok := m.saved[subject]
	if !ok {
		return domain.ErrNotFound
	}

	kept := []domain.Photo{photo}

	for _, old := range got.Photos {
		if old.PhotoMediaID != photo.PhotoMediaID {
			kept = append(kept, old)
		}
	}

	m.setPhotos(got, kept)

	return nil
}

func (m *memoryRepository) RemovePhoto(_ context.Context, subject, id string, _ time.Time) error {
	got, ok := m.saved[subject]
	if !ok {
		return domain.ErrNotFound
	}

	var kept []domain.Photo

	for _, old := range got.Photos {
		if old.PhotoMediaID != id {
			kept = append(kept, old)
		}
	}

	m.setPhotos(got, kept)

	return nil
}

func (m *memoryRepository) setPhotos(got domain.Profile, photos []domain.Photo) {
	got.Photos = photos
	got.Fields.PhotoMediaID = ""
	got.Fields.VideoMediaID = ""

	if len(photos) > 0 {
		got.Fields.PhotoMediaID = photos[0].PhotoMediaID
		got.Fields.VideoMediaID = photos[0].VideoMediaID
	}

	m.saved[got.Subject] = got
}

type hidingGuard struct{}

func (hidingGuard) Filter(_ context.Context, viewer string, target domain.Profile) (domain.Profile, error) {
	if viewer != target.Subject {
		target.Fields.Phone = ""
	}

	return target, nil
}

type memoryFiles map[string]media.Object

func (m memoryFiles) Get(_ context.Context, id string) (media.Object, error) {
	object, ok := m[id]
	if !ok {
		return media.Object{}, media.ErrNotFound
	}

	return object, nil
}

var testFiles = memoryFiles{
	"own-photo":   {ID: "own-photo", Owner: "anna", Size: 1 << 20},
	"own-video":   {ID: "own-video", Owner: "anna", Size: 20 << 20},
	"large-photo": {ID: "large-photo", Owner: "anna", Size: 11 << 20},
	"large-video": {ID: "large-video", Owner: "anna", Size: 51 << 20},
	"own-second":  {ID: "own-second", Owner: "anna", Size: 1 << 20},
	"foreign":     {ID: "foreign", Owner: "boris", Size: 1 << 20},
}

type countingNotifier struct {
	notified map[string]int
}

func (n *countingNotifier) Notify(subject string) {
	n.notified[subject]++
}

func newService() (*profile.Service, *memoryRepository) {
	service, repository, _ := newNotifyingService()

	return service, repository
}

func newNotifyingService() (*profile.Service, *memoryRepository, *countingNotifier) {
	repository := &memoryRepository{saved: map[string]domain.Profile{}}
	notifier := &countingNotifier{notified: map[string]int{}}
	now := time.Unix(100, 0)

	return profile.New(repository, testFiles, hidingGuard{}, notifier, func() time.Time {
		return now
	}), repository, notifier
}

func TestOnlyASavedChangeWakesTheOwner(t *testing.T) {
	t.Parallel()

	service, _, notifier := newNotifyingService()
	principal := identity.Principal{Subject: "anna", Scopes: []string{domain.ScopeWrite}}

	if _, err := service.UpdateOwn(context.Background(), principal, domain.Fields{}); err == nil {
		t.Fatal("an invalid profile was accepted")
	}

	if notifier.notified["anna"] != 0 {
		t.Fatal("woken by a refused change")
	}

	if _, err := service.UpdateOwn(context.Background(), principal, domain.Fields{FirstName: "Анна"}); err != nil {
		t.Fatal(err)
	}

	if notifier.notified["anna"] != 1 {
		t.Fatalf("got %d wake-ups", notifier.notified["anna"])
	}
}

func TestScopesDecideWhatAPersonMayDo(t *testing.T) {
	t.Parallel()

	service, _ := newService()
	ctx := context.Background()
	reader := identity.Principal{Subject: "a", Scopes: []string{domain.ScopeRead}}
	writer := identity.Principal{Subject: "a", Scopes: []string{domain.ScopeWrite}}

	if _, err := service.GetOwn(ctx, writer); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("read without the scope: %v", err)
	}

	if _, err := service.UpdateOwn(ctx, reader, domain.Fields{FirstName: "A"}); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("write without the scope: %v", err)
	}
}

func TestAPersonOnlyEverTouchesTheirOwnProfile(t *testing.T) {
	t.Parallel()

	service, repository := newService()
	ctx := context.Background()
	scopes := []string{domain.ScopeRead, domain.ScopeWrite}
	anna := identity.Principal{Subject: "anna", Scopes: scopes}
	petro := identity.Principal{Subject: "petro", Scopes: scopes}

	if _, err := service.UpdateOwn(ctx, anna, domain.Fields{FirstName: "Анна"}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.GetOwn(ctx, petro); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("another person's profile: %v", err)
	}

	if repository.saved["anna"].UpdatedAt.Unix() != 100 {
		t.Fatalf("the clock was not used: %+v", repository.saved["anna"])
	}
}

func TestAPublicProfileNeedsTheRightToRead(t *testing.T) {
	t.Parallel()

	service, _ := newService()
	ctx := context.Background()
	anna := identity.Principal{Subject: "anna", Scopes: []string{domain.ScopeWrite}}
	petro := identity.Principal{Subject: "petro", Scopes: []string{domain.ScopeRead}}

	if _, err := service.UpdateOwn(ctx, anna, domain.Fields{FirstName: "Анна"}); err != nil {
		t.Fatal(err)
	}

	got, err := service.GetPublic(ctx, petro, "anna")
	if err != nil || got.Fields.FirstName != "Анна" {
		t.Fatalf("got %+v, %v", got, err)
	}

	if _, err := service.GetPublic(ctx, anna, "anna"); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("without the scope: %v", err)
	}

	if _, err := service.GetPublic(ctx, petro, "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a missing person: %v", err)
	}
}

func TestEveryNotifierIsWoken(t *testing.T) {
	t.Parallel()

	first := &countingNotifier{notified: map[string]int{}}
	second := &countingNotifier{notified: map[string]int{}}

	profile.Notifiers{first, second}.Notify("anna")

	if first.notified["anna"] != 1 || second.notified["anna"] != 1 {
		t.Fatalf("got %v and %v", first.notified, second.notified)
	}
}

func TestAnInvalidProfileIsNotSaved(t *testing.T) {
	t.Parallel()

	service, repository := newService()
	principal := identity.Principal{Subject: "a", Scopes: []string{domain.ScopeWrite}}

	_, err := service.UpdateOwn(context.Background(), principal, domain.Fields{})
	if !errors.Is(err, domain.ErrInvalidName) {
		t.Fatalf("got %v", err)
	}

	if len(repository.saved) != 0 {
		t.Fatal("saved anyway")
	}
}

func TestOnlyOwnFilesWithinLimitsBecomeAPhoto(t *testing.T) {
	t.Parallel()

	principal := identity.Principal{Subject: "anna", Scopes: []string{domain.ScopeWrite}}

	for name, tc := range map[string]struct {
		photo, video string
		valid        bool
	}{
		"photo":           {photo: "own-photo", valid: true},
		"photo and video": {photo: "own-photo", video: "own-video", valid: true},
		"no photo":        {video: "own-video"},
		"foreign photo":   {photo: "foreign"},
		"foreign video":   {photo: "own-photo", video: "foreign"},
		"missing photo":   {photo: "missing"},
		"too large photo": {photo: "large-photo"},
		"too large video": {photo: "own-photo", video: "large-video"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			service, _ := newService()
			ctx := context.Background()

			if _, err := service.UpdateOwn(ctx, principal, domain.Fields{FirstName: "Анна"}); err != nil {
				t.Fatal(err)
			}

			_, err := service.AddPhoto(ctx, principal, tc.photo, tc.video)
			if tc.valid && err != nil {
				t.Fatal(err)
			}

			if !tc.valid && !errors.Is(err, domain.ErrInvalidMedia) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestTheNewestPhotoIsTheAvatarAndEditingKeepsThem(t *testing.T) {
	t.Parallel()

	service, _ := newService()
	ctx := context.Background()
	anna := identity.Principal{Subject: "anna", Scopes: []string{domain.ScopeRead, domain.ScopeWrite}}

	if _, err := service.UpdateOwn(ctx, anna, domain.Fields{FirstName: "Анна"}); err != nil {
		t.Fatal(err)
	}

	if _, err := service.AddPhoto(ctx, anna, "own-photo", ""); err != nil {
		t.Fatal(err)
	}

	got, err := service.AddPhoto(ctx, anna, "own-second", "")
	if err != nil {
		t.Fatal(err)
	}

	if got.Fields.PhotoMediaID != "own-second" || len(got.Photos) != 2 {
		t.Fatalf("after adding: %+v", got)
	}

	got, err = service.UpdateOwn(ctx, anna, domain.Fields{FirstName: "Аня", PhotoMediaID: "foreign"})
	if err != nil || got.Fields.PhotoMediaID != "own-second" || len(got.Photos) != 2 {
		t.Fatalf("after editing: %+v, %v", got, err)
	}

	got, err = service.AddPhoto(ctx, anna, "own-photo", "")
	if err != nil || got.Fields.PhotoMediaID != "own-photo" || len(got.Photos) != 2 {
		t.Fatalf("made main: %+v, %v", got, err)
	}

	got, err = service.RemovePhoto(ctx, anna, "own-photo")
	if err != nil || got.Fields.PhotoMediaID != "own-second" || len(got.Photos) != 1 {
		t.Fatalf("removed: %+v, %v", got, err)
	}
}

func TestPhotosNeedTheWriteScope(t *testing.T) {
	t.Parallel()

	service, _ := newService()
	reader := identity.Principal{Subject: "anna", Scopes: []string{domain.ScopeRead}}

	if _, err := service.AddPhoto(context.Background(), reader, "own-photo", ""); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("add: %v", err)
	}

	if _, err := service.RemovePhoto(context.Background(), reader, "own-photo"); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("remove: %v", err)
	}
}

func TestThePhoneIsSeenOnlyByItsOwner(t *testing.T) {
	t.Parallel()

	service, _ := newService()
	anna := identity.Principal{Subject: "anna", Scopes: []string{domain.ScopeRead, domain.ScopeWrite}}
	boris := identity.Principal{Subject: "boris", Scopes: []string{domain.ScopeRead}}

	_, err := service.UpdateOwn(context.Background(), anna, domain.Fields{FirstName: "Анна", Phone: "12345"})
	if err != nil {
		t.Fatal(err)
	}

	own, err := service.GetPublic(context.Background(), anna, "anna")
	if err != nil || own.Fields.Phone != "12345" {
		t.Fatalf("own profile: %+v, %v", own, err)
	}

	other, err := service.GetPublic(context.Background(), boris, "anna")
	if err != nil || other.Fields.Phone != "" {
		t.Fatalf("another person's view: %+v, %v", other, err)
	}
}
