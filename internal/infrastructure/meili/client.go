package meili

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.airlance.org/server/internal/domain/directory"
)

const (
	indexName     = "directory"
	kindPerson    = "person"
	requestLimit  = 5 * time.Second
	responseLimit = 4 << 20
	idBytes       = 16
)

type Client struct {
	url  string
	key  string
	http *http.Client
}

type document struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Subject      string `json:"subject"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Username     string `json:"username"`
	PhotoMediaID string `json:"photo_media_id"`
	VideoMediaID string `json:"video_media_id"`
	Hidden       bool   `json:"hidden"`
}

func New(url, key string) *Client {
	return &Client{url: strings.TrimRight(url, "/"), key: key, http: &http.Client{Timeout: requestLimit}}
}

func (c *Client) Configure(ctx context.Context) error {
	return c.send(ctx, http.MethodPatch, "/indexes/"+indexName+"/settings", map[string]any{
		"searchableAttributes": []string{"username", "first_name", "last_name"},
		"filterableAttributes": []string{"kind", "hidden"},
		"displayedAttributes": []string{
			"kind", "subject", "first_name", "last_name", "username", "photo_media_id", "video_media_id",
		},
	}, nil)
}

func (c *Client) Upsert(ctx context.Context, people []directory.Person) error {
	documents := make([]document, len(people))

	for index, person := range people {
		documents[index] = document{
			ID:           documentID(person.Subject),
			Kind:         kindPerson,
			Subject:      person.Subject,
			FirstName:    person.FirstName,
			LastName:     person.LastName,
			Username:     person.Username,
			PhotoMediaID: person.PhotoMediaID,
			VideoMediaID: person.VideoMediaID,
			Hidden:       person.IsHidden,
		}
	}

	return c.send(ctx, http.MethodPost, "/indexes/"+indexName+"/documents?primaryKey=id", documents, nil)
}

func (c *Client) Search(ctx context.Context, query string, limit int) ([]directory.Person, error) {
	var found struct {
		Hits []document `json:"hits"`
	}

	err := c.send(ctx, http.MethodPost, "/indexes/"+indexName+"/search", map[string]any{
		"q":      query,
		"limit":  limit,
		"filter": "kind = " + kindPerson + " AND hidden != true",
	}, &found)
	if err != nil {
		return nil, err
	}

	people := make([]directory.Person, len(found.Hits))

	for index, hit := range found.Hits {
		people[index] = directory.Person{
			Subject:      hit.Subject,
			FirstName:    hit.FirstName,
			LastName:     hit.LastName,
			Username:     hit.Username,
			PhotoMediaID: hit.PhotoMediaID,
			VideoMediaID: hit.VideoMediaID,
		}
	}

	return people, nil
}

func documentID(subject string) string {
	sum := sha256.Sum256([]byte(subject))

	return "p" + hex.EncodeToString(sum[:idBytes])
}

func (c *Client) send(ctx context.Context, method, path string, body, reply any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.url+path, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	request.Header.Set("Authorization", "Bearer "+c.key)
	request.Header.Set("Content-Type", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()

	limited := io.LimitReader(response.Body, responseLimit)
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		var failure struct {
			Code string `json:"code"`
		}

		_ = json.NewDecoder(limited).Decode(&failure)

		return errors.New("meilisearch answered " + response.Status + ": " + failure.Code)
	}

	if reply == nil {
		return nil
	}

	if err := json.NewDecoder(limited).Decode(reply); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}
