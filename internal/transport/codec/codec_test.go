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
	}
}
