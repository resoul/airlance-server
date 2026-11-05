package codec_test

import (
	"errors"
	"math/rand/v2"
	"reflect"
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

	fields := codec.ProfileFields{
		FirstName:    "Анна",
		LastName:     "Ш",
		Username:     "anna_s",
		Bio:          "Привет",
		Phone:        "+380",
		PhotoMediaID: "photo",
		VideoMediaID: "video",
	}

	gotFields, err := codec.DecodeProfileSet(codec.EncodeProfileSet(fields))
	if err != nil || gotFields != fields {
		t.Fatalf("got %+v, %v", gotFields, err)
	}

	data := codec.ProfileData{Subject: "s", Fields: fields, UpdatedAtUnix: 5, Photos: []codec.ProfilePhoto{
		{PhotoMediaID: "p1", VideoMediaID: "v1", AddedAtUnix: 3},
		{PhotoMediaID: "p2", AddedAtUnix: 2},
	}}

	gotData, err := codec.DecodeProfileData(codec.EncodeProfileData(data))
	if err != nil || !reflect.DeepEqual(gotData, data) {
		t.Fatalf("got %+v, %v", gotData, err)
	}

	photo, video, err := codec.DecodeProfilePhotoAdd(codec.EncodeProfilePhotoAdd("p", "v"))
	if err != nil || photo != "p" || video != "v" {
		t.Fatalf("add: %q, %q, %v", photo, video, err)
	}

	removed, err := codec.DecodeProfilePhotoRemove(codec.EncodeProfilePhotoRemove("p"))
	if err != nil || removed != "p" {
		t.Fatalf("remove: %q, %v", removed, err)
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
		{Subject: "a", FirstName: "Анна", LastName: "Ш", Username: "anna", PhotoMediaID: "p", VideoMediaID: "v"},
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

func TestStoryBodiesRoundTrip(t *testing.T) {
	t.Parallel()

	publish := codec.StoryPublish{
		ClientID:      "c",
		MediaID:       "m",
		CoverMediaID:  "k",
		IsVideo:       true,
		LifetimeHours: 48,
		Meta:          "{}",
	}

	gotPublish, err := codec.DecodeStoryPublish(codec.EncodeStoryPublish(publish))
	if err != nil || gotPublish != publish {
		t.Fatalf("publish: %+v, %v", gotPublish, err)
	}

	data := codec.StoryData{
		StoryID:           "s",
		ClientID:          "c",
		OwnerSubject:      "a",
		OwnerFirstName:    "Анна",
		OwnerLastName:     "Ш",
		OwnerPhotoMediaID: "p",
		OwnerVideoMediaID: "v",
		MediaID:           "m",
		CoverMediaID:      "k",
		IsVideo:           true,
		Meta:              "{}",
		CreatedAtUnix:     1,
		ExpiresAtUnix:     2,
		Seen:              true,
		ViewCount:         3,
	}

	gotData, err := codec.DecodeStoryData(codec.EncodeStoryData(data))
	if err != nil || gotData != data {
		t.Fatalf("data: %+v, %v", gotData, err)
	}

	list, err := codec.DecodeStoriesData(codec.EncodeStoriesData([]codec.StoryData{data, data}))
	if err != nil || len(list) != 2 || list[1] != data {
		t.Fatalf("list: %+v, %v", list, err)
	}

	id, err := codec.DecodeStoryView(codec.EncodeStoryView("s"))
	if err != nil || id != "s" {
		t.Fatalf("view: %q, %v", id, err)
	}

	id, err = codec.DecodeStoryDelete(codec.EncodeStoryDelete("s"))
	if err != nil || id != "s" {
		t.Fatalf("delete: %q, %v", id, err)
	}
}

func TestPrivacyBodiesRoundTrip(t *testing.T) {
	t.Parallel()

	settings := codec.PrivacySettings{Phone: 2, Photos: 1, Bio: 1, Messages: 1, Search: 2}

	set, err := codec.DecodePrivacySet(codec.EncodePrivacySet(settings))
	if err != nil || set != settings {
		t.Fatalf("set: %+v, %v", set, err)
	}

	data, err := codec.DecodePrivacyData(codec.EncodePrivacyData(settings))
	if err != nil || data != settings {
		t.Fatalf("data: %+v, %v", data, err)
	}

	subject, blocked, err := codec.DecodeBlockSet(codec.EncodeBlockSet("a", true))
	if err != nil || subject != "a" || !blocked {
		t.Fatalf("block: %q, %v, %v", subject, blocked, err)
	}

	people, err := codec.DecodeBlocksData(codec.EncodeBlocksData([]codec.SearchPerson{{Subject: "a", FirstName: "А"}}))
	if err != nil || len(people) != 1 || people[0].FirstName != "А" {
		t.Fatalf("blocks: %+v, %v", people, err)
	}
}

func TestContactBodiesRoundTrip(t *testing.T) {
	t.Parallel()

	row := codec.ContactData{
		Subject:         "a",
		FirstName:       "Анна",
		LastName:        "Ш",
		Username:        "anna",
		PhotoMediaID:    "p",
		VideoMediaID:    "v",
		CustomFirstName: "Аня",
		CustomLastName:  "К",
		Phone:           "380",
		FromDevice:      true,
	}

	list, err := codec.DecodeContactsData(codec.EncodeContactsData([]codec.ContactData{row, row}))
	if err != nil || len(list) != 2 || list[1] != row {
		t.Fatalf("list: %+v, %v", list, err)
	}

	add := codec.ContactAdd{Target: "a", FirstName: "Аня", LastName: "К"}

	gotAdd, err := codec.DecodeContactAdd(codec.EncodeContactAdd(add))
	if err != nil || gotAdd != add {
		t.Fatalf("add: %+v, %v", gotAdd, err)
	}

	gotPhone, err := codec.DecodeContactAddPhone(codec.EncodeContactAddPhone(add))
	if err != nil || gotPhone != add {
		t.Fatalf("add by phone: %+v, %v", gotPhone, err)
	}

	subject, err := codec.DecodeContactRemove(codec.EncodeContactRemove("a"))
	if err != nil || subject != "a" {
		t.Fatalf("remove: %q, %v", subject, err)
	}

	phones, err := codec.DecodeContactsSync(codec.EncodeContactsSync([]string{"1", "2"}))
	if err != nil || len(phones) != 2 || phones[1] != "2" {
		t.Fatalf("sync: %+v, %v", phones, err)
	}
}
