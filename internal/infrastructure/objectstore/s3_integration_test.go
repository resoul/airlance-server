//go:build integration

package objectstore_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"go.airlance.org/server/internal/infrastructure/objectstore"
)

func request(t *testing.T, method, url string, body []byte) (int, []byte) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	read, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}

	return res.StatusCode, read
}

func TestSignedLinksAgainstS3(t *testing.T) {
	endpoint := os.Getenv("AIRLANCE_SERVER_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("AIRLANCE_SERVER_TEST_S3_ENDPOINT is not set")
	}

	store, err := objectstore.New(objectstore.Config{
		Endpoint:  endpoint,
		PublicURL: endpoint,
		AccessKey: os.Getenv("AIRLANCE_SERVER_TEST_S3_ACCESS_KEY"),
		SecretKey: os.Getenv("AIRLANCE_SERVER_TEST_S3_SECRET_KEY"),
		Bucket:    "test-media",
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := store.EnsureBucket(ctx); err != nil {
		t.Fatal(err)
	}

	if err := store.EnsureBucket(ctx); err != nil {
		t.Fatalf("a bucket that already exists: %v", err)
	}

	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}

	id := hex.EncodeToString(raw)
	payload := []byte("hello airlance")

	upload, err := store.UploadURL(ctx, id, int64(len(payload)), time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if status, _ := request(t, http.MethodPut, upload, append(bytes.Clone(payload), 'x')); status != http.StatusForbidden {
		t.Fatalf("a longer file than promised: %d", status)
	}

	if status, _ := request(t, http.MethodPut, upload, payload); status != http.StatusOK {
		t.Fatalf("upload: %d", status)
	}

	download, err := store.DownloadURL(ctx, id, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	status, got := request(t, http.MethodGet, download, nil)
	if status != http.StatusOK || !bytes.Equal(got, payload) {
		t.Fatalf("download: %d, %q", status, got)
	}

	if status, _ := request(t, http.MethodGet, endpoint+"/test-media/"+id, nil); status != http.StatusForbidden {
		t.Fatalf("a read without a signature: %d", status)
	}
}
