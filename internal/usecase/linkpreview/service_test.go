package linkpreview_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.airlance.org/server/internal/domain/identity"
	domain "go.airlance.org/server/internal/domain/linkpreview"
	"go.airlance.org/server/internal/domain/media"
	"go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/usecase/linkpreview"
)

type memory struct {
	saved map[string]domain.Preview
	at    map[string]time.Time
}

func (m *memory) Get(_ context.Context, link string) (domain.Preview, time.Time, bool, error) {
	preview, found := m.saved[link]

	return preview, m.at[link], found, nil
}

func (m *memory) Save(_ context.Context, preview domain.Preview, now time.Time) error {
	m.saved[preview.URL], m.at[preview.URL] = preview, now

	return nil
}

type fetcher struct {
	page  linkpreview.Page
	err   error
	calls int
}

func (f *fetcher) Page(context.Context, string) (linkpreview.Page, error) {
	f.calls++

	return f.page, f.err
}

type blobs struct{ put map[string][]byte }

func (b *blobs) Put(_ context.Context, id string, data []byte) error {
	b.put[id] = data

	return nil
}

type registry struct{ created []media.Object }

func (r *registry) Create(_ context.Context, object media.Object) error {
	r.created = append(r.created, object)

	return nil
}

var anna = identity.Principal{Subject: "anna", Scopes: []string{message.ScopeRead}}

type fixture struct {
	service *linkpreview.Service
	repo    *memory
	fetch   *fetcher
	blobs   *blobs
	reg     *registry
	clock   *time.Time
}

func newFixture() fixture {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	f := fixture{
		repo:  &memory{saved: map[string]domain.Preview{}, at: map[string]time.Time{}},
		fetch: &fetcher{page: linkpreview.Page{Site: "Site", Title: "Title", Summary: "About"}},
		blobs: &blobs{put: map[string][]byte{}},
		reg:   &registry{},
		clock: &now,
	}
	f.service = linkpreview.New(f.repo, f.fetch, f.blobs, f.reg, func() time.Time { return *f.clock })

	return f
}

func TestAPreviewIsBuiltOnceAndThenServedFromTheCache(t *testing.T) {
	t.Parallel()

	f := newFixture()
	f.fetch.page.Image, f.fetch.page.Width, f.fetch.page.Height = []byte("jpeg"), 640, 320

	first, err := f.service.Preview(context.Background(), anna, " https://Example.com/a#part ")
	if err != nil || !first.Found || first.Title != "Title" || first.PhotoMediaID == "" || first.Width != 640 {
		t.Fatalf("got %+v, %v", first, err)
	}

	if len(f.blobs.put) != 1 || len(f.reg.created) != 1 || f.reg.created[0].ID != first.PhotoMediaID {
		t.Fatalf("the image was not stored: %v, %v", f.blobs.put, f.reg.created)
	}

	second, err := f.service.Preview(context.Background(), anna, "https://example.com/a")
	if err != nil || second != first || f.fetch.calls != 1 {
		t.Fatalf("got %+v, %v after %d fetches", second, err, f.fetch.calls)
	}

	*f.clock = f.clock.Add(25 * time.Hour)
	if _, err := f.service.Preview(context.Background(), anna, "https://example.com/a"); err != nil || f.fetch.calls != 2 {
		t.Fatalf("an old preview was not refreshed: %v, %d", err, f.fetch.calls)
	}
}

func TestAFailedPageIsRememberedForAWhile(t *testing.T) {
	t.Parallel()

	f := newFixture()
	f.fetch.err = errors.New("down")

	got, err := f.service.Preview(context.Background(), anna, "https://example.com/x")
	if err != nil || got.Found {
		t.Fatalf("got %+v, %v", got, err)
	}

	if _, err := f.service.Preview(context.Background(), anna, "https://example.com/x"); err != nil || f.fetch.calls != 1 {
		t.Fatalf("a failure was fetched again at once: %v, %d", err, f.fetch.calls)
	}

	*f.clock = f.clock.Add(16 * time.Minute)
	f.fetch.err = nil

	if got, err := f.service.Preview(context.Background(), anna, "https://example.com/x"); err != nil || !got.Found {
		t.Fatalf("a recovered page: %+v, %v", got, err)
	}
}

func TestBadLinksScopesAndFloodsAreRefused(t *testing.T) {
	t.Parallel()

	f := newFixture()

	for _, raw := range []string{"", "ftp://example.com", "javascript:alert(1)", "https://u:p@example.com", "https://example.com:8080", "not a url"} {
		if _, err := f.service.Preview(context.Background(), anna, raw); !errors.Is(err, domain.ErrInvalidURL) {
			t.Errorf("%q: %v", raw, err)
		}
	}

	if _, err := f.service.Preview(context.Background(), identity.Principal{Subject: "x"}, "https://example.com"); !errors.Is(err, identity.ErrForbidden) {
		t.Fatalf("a person without the scope: %v", err)
	}

	var limited error

	for index := range 40 {
		_, limited = f.service.Preview(context.Background(), anna, "https://example.com/"+string(rune('a'+index%26))+string(rune('a'+index/26)))
		if limited != nil {
			break
		}
	}

	if !errors.Is(limited, domain.ErrRateLimited) {
		t.Fatalf("no limit: %v", limited)
	}
}
