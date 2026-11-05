package codec_test

import (
	"errors"
	"math/rand/v2"
	"testing"

	"go.airlance.org/server/internal/transport/codec"
)

func TestFramesRoundTrip(t *testing.T) {
	t.Parallel()

	want := codec.Frame{Seq: 7, RequestID: 9, Kind: codec.KindPing, Body: codec.EncodePing(42)}

	got, err := codec.DecodeFrame(codec.EncodeFrame(want))
	if err != nil {
		t.Fatal(err)
	}

	if got.Seq != 7 || got.RequestID != 9 || got.Kind != codec.KindPing {
		t.Fatalf("got %+v", got)
	}

	id, err := codec.DecodePing(got.Body)
	if err != nil || id != 42 {
		t.Fatalf("got %d, %v", id, err)
	}
}

func TestProfileBodiesRoundTrip(t *testing.T) {
	t.Parallel()

	fields := codec.ProfileFields{FirstName: "Анна", LastName: "Ш", Username: "anna_s", Bio: "Привет"}

	gotFields, err := codec.DecodeProfileSet(codec.EncodeProfileSet(fields))
	if err != nil || gotFields != fields {
		t.Fatalf("got %+v, %v", gotFields, err)
	}

	data := codec.ProfileData{Subject: "s", Fields: fields, UpdatedAtUnix: 5}

	gotData, err := codec.DecodeProfileData(codec.EncodeProfileData(data))
	if err != nil || gotData != data {
		t.Fatalf("got %+v, %v", gotData, err)
	}
}

func TestUpdatesBodiesRoundTrip(t *testing.T) {
	t.Parallel()

	after, err := codec.DecodeUpdatesSubscribe(codec.EncodeUpdatesSubscribe(41))
	if err != nil || after != 41 {
		t.Fatalf("got %d, %v", after, err)
	}

	latest, err := codec.DecodeUpdatesSubscribed(codec.EncodeUpdatesSubscribed(42))
	if err != nil || latest != 42 {
		t.Fatalf("got %d, %v", latest, err)
	}
}

func TestMediaBodiesRoundTrip(t *testing.T) {
	t.Parallel()

	size, err := codec.DecodeMediaUploadRequest(codec.EncodeMediaUploadRequest(77))
	if err != nil || size != 77 {
		t.Fatalf("got %d, %v", size, err)
	}

	upload := codec.MediaUploadTicket{MediaID: "one", URL: "https://files/one", ExpiresAtUnix: 5}

	gotUpload, err := codec.DecodeMediaUploadTicket(codec.EncodeMediaUploadTicket(upload))
	if err != nil || gotUpload != upload {
		t.Fatalf("got %+v, %v", gotUpload, err)
	}

	id, err := codec.DecodeMediaDownloadRequest(codec.EncodeMediaDownloadRequest("one"))
	if err != nil || id != "one" {
		t.Fatalf("got %q, %v", id, err)
	}

	download := codec.MediaDownloadTicket{URL: "https://files/one?get", ExpiresAtUnix: 6}

	gotDownload, err := codec.DecodeMediaDownloadTicket(codec.EncodeMediaDownloadTicket(download))
	if err != nil || gotDownload != download {
		t.Fatalf("got %+v, %v", gotDownload, err)
	}
}

func TestSearchBodiesRoundTrip(t *testing.T) {
	t.Parallel()

	query, err := codec.DecodeSearch(codec.EncodeSearch("Шевч"))
	if err != nil || query != "Шевч" {
		t.Fatalf("got %q, %v", query, err)
	}

	want := []codec.SearchPerson{
		{Subject: "a", FirstName: "Анна", LastName: "Ш", Username: "anna"},
		{Subject: "b", FirstName: "Петро"},
	}

	got, err := codec.DecodeSearchResults(codec.EncodeSearchResults(want))
	if err != nil || len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %+v, %v", got, err)
	}

	empty, err := codec.DecodeSearchResults(codec.EncodeSearchResults(nil))
	if err != nil || len(empty) != 0 {
		t.Fatalf("got %+v, %v", empty, err)
	}

	subject, err := codec.DecodePublicProfileGet(codec.EncodePublicProfileGet("a"))
	if err != nil || subject != "a" {
		t.Fatalf("got %q, %v", subject, err)
	}
}

func TestBrokenFramesAreRefused(t *testing.T) {
	t.Parallel()

	good := codec.EncodeFrame(codec.Frame{Kind: codec.KindPing, Body: codec.EncodePing(1)})
	wrongIdentifier := append([]byte(nil), good...)
	wrongIdentifier[4] = 'X'

	for name, data := range map[string][]byte{
		"empty":            {},
		"short":            {1, 2, 3},
		"wrong identifier": wrongIdentifier,
		"too large":        make([]byte, codec.MaxFrameBytes+1),
	} {
		if _, err := codec.DecodeFrame(data); !errors.Is(err, codec.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestDamagedBuffersNeverPanic(t *testing.T) {
	t.Parallel()

	good := codec.EncodeFrame(codec.Frame{
		Kind: codec.KindProfileSet,
		Body: codec.EncodeProfileSet(codec.ProfileFields{FirstName: "Анна", Username: "anna_s"}),
	})
	random := rand.New(rand.NewPCG(1, 2))

	for range 20_000 {
		damaged := append([]byte(nil), good...)
		for range 1 + random.IntN(8) {
			damaged[random.IntN(len(damaged))] = byte(random.IntN(256))
		}

		frame, err := codec.DecodeFrame(damaged)
		if err != nil {
			continue
		}

		_, _ = codec.DecodeProfileSet(frame.Body)
		_, _ = codec.DecodePing(frame.Body)
		_, _ = codec.DecodeProfileData(frame.Body)
		_, _ = codec.DecodeUpdatesSubscribe(frame.Body)
		_, _ = codec.DecodeMediaUploadRequest(frame.Body)
		_, _ = codec.DecodeMediaDownloadRequest(frame.Body)
		_, _ = codec.DecodeSearch(frame.Body)
		_, _ = codec.DecodeSearchResults(frame.Body)
		_, _ = codec.DecodePublicProfileGet(frame.Body)
	}
}
