package ws_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.airlance.org/server/internal/domain/directory"
	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/transport/codec"
	"go.airlance.org/server/internal/transport/ws"
)

type fakeDirectory struct{}

func (fakeDirectory) Search(_ context.Context, _ identity.Principal, query string) ([]directory.Person, error) {
	switch query {
	case "ab":
		return nil, directory.ErrInvalidQuery
	case "fast":
		return nil, directory.ErrTooManySearches
	case "denied":
		return nil, identity.ErrForbidden
	case "down":
		return nil, errors.Join(directory.ErrSearchUnavailable, errors.New("secret index detail"))
	case "boom":
		return nil, errors.New("secret detail")
	case "nobody":
		return nil, nil
	}

	return []directory.Person{
		{Subject: "petro", FirstName: "Петро", LastName: "Ш", Username: "petro"},
		{Subject: "olena", FirstName: "Олена"},
	}, nil
}

func searchFrame(id uint64, query string) codec.Frame {
	return codec.Frame{RequestID: id, Kind: codec.KindSearch, Body: codec.EncodeSearch(query)}
}

func TestSearchReturnsPeople(t *testing.T) {
	t.Parallel()

	c := signedIn(t, start(t, time.Minute, ws.DefaultLimits()))

	response := c.ask(searchFrame(2, "pet"))
	people, err := codec.DecodeSearchResults(response.Body)
	if response.Kind != codec.KindSearchResults || err != nil || len(people) != 2 {
		t.Fatalf("got %+v, %+v, %v", response, people, err)
	}

	want := codec.SearchPerson{Subject: "petro", FirstName: "Петро", LastName: "Ш", Username: "petro"}
	if people[0] != want || people[1].Subject != "olena" {
		t.Fatalf("got %+v", people)
	}

	empty := c.ask(searchFrame(3, "nobody"))
	people, err = codec.DecodeSearchResults(empty.Body)
	if empty.Kind != codec.KindSearchResults || err != nil || len(people) != 0 {
		t.Fatalf("got %+v, %+v, %v", empty, people, err)
	}
}

func TestSearchFailuresGiveOnlyACode(t *testing.T) {
	t.Parallel()

	c := signedIn(t, start(t, time.Minute, ws.DefaultLimits()))

	assertError(t, c.ask(searchFrame(2, "ab")), 422, "invalid_query")
	assertError(t, c.ask(searchFrame(3, "fast")), 429, "too_many_searches")
	assertError(t, c.ask(searchFrame(4, "denied")), 403, "forbidden")
	assertError(t, c.ask(searchFrame(5, "down")), 503, "search_unavailable")
	assertError(t, c.ask(searchFrame(6, "boom")), 500, "internal")

	broken := codec.Frame{RequestID: 7, Kind: codec.KindSearch, Body: []byte{1}}
	assertError(t, c.ask(broken), 400, "invalid body")
}

func TestAPublicProfileAnswersLikeAProfile(t *testing.T) {
	t.Parallel()

	c := signedIn(t, start(t, time.Minute, ws.DefaultLimits()))

	request := func(id uint64, subject string) codec.Frame {
		return codec.Frame{RequestID: id, Kind: codec.KindPublicProfileGet, Body: codec.EncodePublicProfileGet(subject)}
	}

	response := c.ask(request(2, "petro"))
	data, err := codec.DecodeProfileData(response.Body)
	if response.Kind != codec.KindProfileData || err != nil || data.Subject != "petro" {
		t.Fatalf("got %+v, %+v, %v", response, data, err)
	}

	assertError(t, c.ask(request(3, "missing")), 404, "profile_not_found")
}
