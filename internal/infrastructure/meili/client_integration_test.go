//go:build integration

package meili_test

import (
	"context"
	"os"
	"testing"
	"time"

	"go.airlance.org/server/internal/domain/directory"
	"go.airlance.org/server/internal/infrastructure/meili"
)

func searchUntil(t *testing.T, client *meili.Client, query string, check func([]directory.Person) bool) []directory.Person {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)

	for {
		people, err := client.Search(context.Background(), query, 20)
		if err == nil && check(people) {
			return people
		}

		if time.Now().After(deadline) {
			t.Fatalf("%q: got %+v, %v", query, people, err)
		}

		time.Sleep(100 * time.Millisecond)
	}
}

func hasSubject(subject string) func([]directory.Person) bool {
	return func(people []directory.Person) bool {
		for _, person := range people {
			if person.Subject == subject {
				return true
			}
		}

		return false
	}
}

func TestSearchAgainstMeilisearch(t *testing.T) {
	url := os.Getenv("AIRLANCE_SERVER_TEST_MEILI_URL")
	if url == "" {
		t.Skip("AIRLANCE_SERVER_TEST_MEILI_URL is not set")
	}

	client := meili.New(url, os.Getenv("AIRLANCE_SERVER_TEST_MEILI_KEY"))
	ctx := context.Background()

	if err := client.Configure(ctx); err != nil {
		t.Fatal(err)
	}

	subject := "user|" + time.Now().Format("150405.000")
	person := directory.Person{Subject: subject, FirstName: "Тарас", LastName: "Шевченкович", Username: "zyxkobzar"}

	if err := client.Upsert(ctx, []directory.Person{person}); err != nil {
		t.Fatal(err)
	}

	found := searchUntil(t, client, "zyxkob", hasSubject(subject))
	if found[0] != person {
		t.Fatalf("got %+v", found[0])
	}

	searchUntil(t, client, "Шевченкович", hasSubject(subject))
	searchUntil(t, client, "zyxkobar", hasSubject(subject))

	person.Username = "qwvnewname"
	if err := client.Upsert(ctx, []directory.Person{person}); err != nil {
		t.Fatal(err)
	}

	searchUntil(t, client, "qwvnewname", hasSubject(subject))
	searchUntil(t, client, "zyxkobzar", func(people []directory.Person) bool {
		return !hasSubject(subject)(people)
	})

	wrong := meili.New(url, "wrong-key")
	if _, err := wrong.Search(ctx, "zyxkob", 20); err == nil {
		t.Fatal("a wrong key was accepted")
	}
}
