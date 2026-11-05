package httpapi_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.airlance.org/server/internal/domain/directory"
	"go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/transport/httpapi"
)

type fakePreviews struct {
	client string
}

func (f *fakePreviews) PreviewChat(_ context.Context, client, code string) (message.Chat, error) {
	f.client = client

	switch code {
	case "news_channel":
		return message.Chat{Kind: message.KindChannel, Title: "<b>Новости</b>", About: "О главном", MemberCount: 7}, nil
	case "secretToken":
		return message.Chat{Kind: message.KindGroup, Title: "Семья", MemberCount: 3}, nil
	case "busy":
		return message.Chat{}, directory.ErrTooManySearches
	default:
		return message.Chat{}, message.ErrChatNotFound
	}
}

func (f *fakePreviews) PreviewUser(_ context.Context, _, username string) (directory.Person, error) {
	if username != "anna" {
		return directory.Person{}, directory.ErrNotFound
	}

	return directory.Person{FirstName: "Анна", LastName: "Коваль", Username: "anna"}, nil
}

func request(t *testing.T, previews *fakePreviews, appID, path string) *httptest.ResponseRecorder {
	t.Helper()

	router := httpapi.NewRouter(
		func(context.Context) error {
			return nil
		},
		http.NotFoundHandler(),
		httpapi.Links{Previews: previews, AppleAppID: appID, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))},
		httpapi.Hooks{},
	)
	incoming := httptest.NewRequest(http.MethodGet, path, http.NoBody)
	incoming.Header.Set("Accept-Language", "ru-RU,ru;q=0.9")
	incoming.Header.Set("CF-Connecting-IP", "203.0.113.9")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, incoming)

	return recorder
}

func TestAPublicLinkOpensTheApp(t *testing.T) {
	t.Parallel()

	previews := &fakePreviews{}
	page := request(t, previews, "", "/c/news_channel")
	body := page.Body.String()

	if page.Code != http.StatusOK || !strings.Contains(body, `href="airlance://link/c/news_channel"`) {
		t.Fatalf("%d: %s", page.Code, body)
	}

	if !strings.Contains(body, "Подписчиков: 7") || !strings.Contains(body, "Открыть в Airlance") {
		t.Fatalf("the texts: %s", body)
	}

	if strings.Contains(body, "<b>Новости") {
		t.Fatalf("a title was not escaped: %s", body)
	}

	if previews.client != "203.0.113.9" {
		t.Fatalf("limited by %q", previews.client)
	}

	headers := page.Header()
	if headers.Get("X-Robots-Tag") != "noindex" || headers.Get("Cache-Control") != "no-store" ||
		headers.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("got %v", headers)
	}
}

func TestAPrivateLinkOffersToJoin(t *testing.T) {
	t.Parallel()

	body := request(t, &fakePreviews{}, "", "/join/secretToken").Body.String()
	if !strings.Contains(body, `href="airlance://link/join/secretToken"`) || !strings.Contains(body, "Вступить") {
		t.Fatal(body)
	}
}

func TestAPersonLinkShowsOnlyTheName(t *testing.T) {
	t.Parallel()

	page := request(t, &fakePreviews{}, "", "/u/anna")
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `href="airlance://link/u/anna"`) {
		t.Fatal(page.Body.String())
	}
}

func TestABadLinkOffersNothing(t *testing.T) {
	t.Parallel()

	for path, status := range map[string]int{
		"/c/missing":             http.StatusNotFound,
		"/u/missing":             http.StatusNotFound,
		"/c/busy":                http.StatusTooManyRequests,
		"/join/%22onclick=alert": http.StatusNotFound,
	} {
		page := request(t, &fakePreviews{}, "", path)
		if page.Code != status || strings.Contains(page.Body.String(), "<a ") {
			t.Errorf("%s: %d: %s", path, page.Code, page.Body.String())
		}
	}
}

func TestAppleFileAppearsOnlyWithAnAppID(t *testing.T) {
	t.Parallel()

	const path = "/.well-known/apple-app-site-association"

	if page := request(t, &fakePreviews{}, "", path); page.Code != http.StatusNotFound {
		t.Fatalf("got %d", page.Code)
	}

	page := request(t, &fakePreviews{}, "TEAM.org.airlance", path)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `"TEAM.org.airlance"`) {
		t.Fatalf("%d: %s", page.Code, page.Body.String())
	}
}
