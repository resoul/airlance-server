package ws_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/media"
	"go.airlance.org/server/internal/transport/codec"
	"go.airlance.org/server/internal/transport/ws"
	mediausecase "go.airlance.org/server/internal/usecase/media"
)

type fakeMedia struct{}

func (fakeMedia) RequestUpload(_ context.Context, _ identity.Principal, size int64) (mediausecase.Ticket, error) {
	switch size {
	case 0:
		return mediausecase.Ticket{}, media.ErrInvalidSize
	case 13:
		return mediausecase.Ticket{}, errors.New("secret store detail")
	}

	return mediausecase.Ticket{MediaID: "one", URL: "https://files/one", ExpiresAt: time.Unix(5, 0)}, nil
}

func (fakeMedia) RequestDownload(_ context.Context, _ identity.Principal, id string) (mediausecase.Ticket, error) {
	if id != "one" {
		return mediausecase.Ticket{}, media.ErrNotFound
	}

	return mediausecase.Ticket{MediaID: id, URL: "https://files/one?get", ExpiresAt: time.Unix(6, 0)}, nil
}

func uploadFrame(id uint64, size int64) codec.Frame {
	return codec.Frame{RequestID: id, Kind: codec.KindMediaUploadRequest, Body: codec.EncodeMediaUploadRequest(size)}
}

func downloadFrame(id uint64, mediaID string) codec.Frame {
	return codec.Frame{
		RequestID: id,
		Kind:      codec.KindMediaDownloadRequest,
		Body:      codec.EncodeMediaDownloadRequest(mediaID),
	}
}

func TestAPersonGetsLinksForFiles(t *testing.T) {
	t.Parallel()

	c := signedIn(t, start(t, time.Minute, ws.DefaultLimits()))

	upload := c.ask(uploadFrame(2, 10))
	ticket, err := codec.DecodeMediaUploadTicket(upload.Body)
	if upload.Kind != codec.KindMediaUploadTicket || err != nil {
		t.Fatalf("got %+v, %v", upload, err)
	}

	if ticket.MediaID != "one" || ticket.URL != "https://files/one" || ticket.ExpiresAtUnix != 5 {
		t.Fatalf("got %+v", ticket)
	}

	download := c.ask(downloadFrame(3, "one"))
	link, err := codec.DecodeMediaDownloadTicket(download.Body)
	if download.Kind != codec.KindMediaDownloadTicket || err != nil {
		t.Fatalf("got %+v, %v", download, err)
	}

	if link.URL != "https://files/one?get" || link.ExpiresAtUnix != 6 {
		t.Fatalf("got %+v", link)
	}
}

func TestMediaFailuresGiveOnlyACode(t *testing.T) {
	t.Parallel()

	c := signedIn(t, start(t, time.Minute, ws.DefaultLimits()))

	assertError(t, c.ask(uploadFrame(2, 0)), 422, "invalid_size")
	assertError(t, c.ask(uploadFrame(3, 13)), 500, "internal")
	assertError(t, c.ask(downloadFrame(4, "missing")), 404, "media_not_found")

	broken := codec.Frame{RequestID: 5, Kind: codec.KindMediaUploadRequest, Body: []byte{1}}
	assertError(t, c.ask(broken), 400, "invalid body")
}
