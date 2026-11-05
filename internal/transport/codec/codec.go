package codec

import (
	"errors"
	"fmt"

	flatbuffers "github.com/google/flatbuffers/go"

	"go.airlance.org/server/internal/transport/wire"
)

const fileIdentifier = "ALNC"

const MaxFrameBytes = 4 << 20

const maxSearchPeople = 256

var ErrInvalid = errors.New("invalid frame")

type Kind uint16

const (
	KindPing              = Kind(wire.MessageKindPing)
	KindPong              = Kind(wire.MessageKindPong)
	KindError             = Kind(wire.MessageKindError)
	KindProfileGet        = Kind(wire.MessageKindProfileGet)
	KindProfileSet        = Kind(wire.MessageKindProfileSet)
	KindProfileData       = Kind(wire.MessageKindProfileData)
	KindAuthenticate      = Kind(wire.MessageKindAuthenticate)
	KindAuthenticated     = Kind(wire.MessageKindAuthenticated)
	KindUpdatesSubscribe  = Kind(wire.MessageKindUpdatesSubscribe)
	KindUpdatesSubscribed = Kind(wire.MessageKindUpdatesSubscribed)

	KindMediaUploadRequest   = Kind(wire.MessageKindMediaUploadRequest)
	KindMediaUploadTicket    = Kind(wire.MessageKindMediaUploadTicket)
	KindMediaDownloadRequest = Kind(wire.MessageKindMediaDownloadRequest)
	KindMediaDownloadTicket  = Kind(wire.MessageKindMediaDownloadTicket)

	KindSearch           = Kind(wire.MessageKindSearch)
	KindSearchResults    = Kind(wire.MessageKindSearchResults)
	KindPublicProfileGet = Kind(wire.MessageKindPublicProfileGet)
)

type SearchPerson struct {
	Subject   string
	FirstName string
	LastName  string
	Username  string
}

type MediaUploadTicket struct {
	MediaID       string
	URL           string
	ExpiresAtUnix int64
}

type MediaDownloadTicket struct {
	URL           string
	ExpiresAtUnix int64
}

type Frame struct {
	Seq       uint64
	RequestID uint64
	Kind      Kind
	Body      []byte
}

type ProfileFields struct {
	FirstName string
	LastName  string
	Username  string
	Bio       string
}

type ProfileData struct {
	Subject       string
	Fields        ProfileFields
	UpdatedAtUnix int64
}

func safely(read func()) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrInvalid
		}
	}()

	read()

	return nil
}

func DecodeFrame(data []byte) (Frame, error) {
	if len(data) > MaxFrameBytes {
		return Frame{}, fmt.Errorf("%w: too large", ErrInvalid)
	}

	var frame Frame

	err := safely(func() {
		if !flatbuffers.BufferHasIdentifier(data, fileIdentifier) {
			panic("identifier")
		}

		envelope := wire.GetRootAsEnvelope(data, 0)
		frame = Frame{
			Seq:       envelope.Seq(),
			RequestID: envelope.RequestId(),
			Kind:      Kind(envelope.Kind()),
			Body:      append([]byte(nil), envelope.BodyBytes()...),
		}
	})
	if err != nil {
		return Frame{}, err
	}

	return frame, nil
}

func EncodeFrame(frame Frame) []byte {
	builder := flatbuffers.NewBuilder(len(frame.Body) + 64)
	body := builder.CreateByteVector(frame.Body)
	wire.EnvelopeStart(builder)
	wire.EnvelopeAddSeq(builder, frame.Seq)
	wire.EnvelopeAddRequestId(builder, frame.RequestID)
	wire.EnvelopeAddKind(builder, uint16(frame.Kind))
	wire.EnvelopeAddBody(builder, body)
	builder.FinishWithFileIdentifier(wire.EnvelopeEnd(builder), []byte(fileIdentifier))

	return builder.FinishedBytes()
}

func DecodeAuthenticate(body []byte) (string, error) {
	var token string

	err := safely(func() {
		token = string(wire.GetRootAsAuthenticate(body, 0).AccessToken())
	})

	return token, err
}

func EncodeAuthenticate(token string) []byte {
	builder := flatbuffers.NewBuilder(len(token) + 32)
	text := builder.CreateString(token)
	wire.AuthenticateStart(builder)
	wire.AuthenticateAddAccessToken(builder, text)
	builder.Finish(wire.AuthenticateEnd(builder))

	return builder.FinishedBytes()
}

func DecodeAuthenticated(body []byte) (int64, error) {
	var expiresAt int64

	err := safely(func() {
		expiresAt = wire.GetRootAsAuthenticated(body, 0).ExpiresAtUnix()
	})

	return expiresAt, err
}

func EncodeAuthenticated(expiresAtUnix int64) []byte {
	builder := flatbuffers.NewBuilder(32)
	wire.AuthenticatedStart(builder)
	wire.AuthenticatedAddExpiresAtUnix(builder, expiresAtUnix)
	builder.Finish(wire.AuthenticatedEnd(builder))

	return builder.FinishedBytes()
}

func DecodeUpdatesSubscribe(body []byte) (uint64, error) {
	var after uint64

	err := safely(func() {
		after = wire.GetRootAsUpdatesSubscribe(body, 0).AfterSeq()
	})

	return after, err
}

func EncodeUpdatesSubscribe(after uint64) []byte {
	builder := flatbuffers.NewBuilder(32)
	wire.UpdatesSubscribeStart(builder)
	wire.UpdatesSubscribeAddAfterSeq(builder, after)
	builder.Finish(wire.UpdatesSubscribeEnd(builder))

	return builder.FinishedBytes()
}

func DecodeUpdatesSubscribed(body []byte) (uint64, error) {
	var latest uint64

	err := safely(func() {
		latest = wire.GetRootAsUpdatesSubscribed(body, 0).LatestSeq()
	})

	return latest, err
}

func EncodeUpdatesSubscribed(latest uint64) []byte {
	builder := flatbuffers.NewBuilder(32)
	wire.UpdatesSubscribedStart(builder)
	wire.UpdatesSubscribedAddLatestSeq(builder, latest)
	builder.Finish(wire.UpdatesSubscribedEnd(builder))

	return builder.FinishedBytes()
}

func DecodeMediaUploadRequest(body []byte) (int64, error) {
	var size int64

	err := safely(func() {
		size = wire.GetRootAsMediaUploadRequest(body, 0).Size()
	})

	return size, err
}

func EncodeMediaUploadRequest(size int64) []byte {
	builder := flatbuffers.NewBuilder(32)
	wire.MediaUploadRequestStart(builder)
	wire.MediaUploadRequestAddSize(builder, size)
	builder.Finish(wire.MediaUploadRequestEnd(builder))

	return builder.FinishedBytes()
}

func DecodeMediaUploadTicket(body []byte) (MediaUploadTicket, error) {
	var ticket MediaUploadTicket

	err := safely(func() {
		table := wire.GetRootAsMediaUploadTicket(body, 0)
		ticket = MediaUploadTicket{
			MediaID:       string(table.MediaId()),
			URL:           string(table.Url()),
			ExpiresAtUnix: table.ExpiresAtUnix(),
		}
	})

	return ticket, err
}

func EncodeMediaUploadTicket(ticket MediaUploadTicket) []byte {
	builder := flatbuffers.NewBuilder(len(ticket.URL) + 128)
	id := builder.CreateString(ticket.MediaID)
	address := builder.CreateString(ticket.URL)
	wire.MediaUploadTicketStart(builder)
	wire.MediaUploadTicketAddMediaId(builder, id)
	wire.MediaUploadTicketAddUrl(builder, address)
	wire.MediaUploadTicketAddExpiresAtUnix(builder, ticket.ExpiresAtUnix)
	builder.Finish(wire.MediaUploadTicketEnd(builder))

	return builder.FinishedBytes()
}

func DecodeMediaDownloadRequest(body []byte) (string, error) {
	var id string

	err := safely(func() {
		id = string(wire.GetRootAsMediaDownloadRequest(body, 0).MediaId())
	})

	return id, err
}

func EncodeMediaDownloadRequest(id string) []byte {
	builder := flatbuffers.NewBuilder(len(id) + 32)
	text := builder.CreateString(id)
	wire.MediaDownloadRequestStart(builder)
	wire.MediaDownloadRequestAddMediaId(builder, text)
	builder.Finish(wire.MediaDownloadRequestEnd(builder))

	return builder.FinishedBytes()
}

func DecodeMediaDownloadTicket(body []byte) (MediaDownloadTicket, error) {
	var ticket MediaDownloadTicket

	err := safely(func() {
		table := wire.GetRootAsMediaDownloadTicket(body, 0)
		ticket = MediaDownloadTicket{URL: string(table.Url()), ExpiresAtUnix: table.ExpiresAtUnix()}
	})

	return ticket, err
}

func EncodeMediaDownloadTicket(ticket MediaDownloadTicket) []byte {
	builder := flatbuffers.NewBuilder(len(ticket.URL) + 64)
	address := builder.CreateString(ticket.URL)
	wire.MediaDownloadTicketStart(builder)
	wire.MediaDownloadTicketAddUrl(builder, address)
	wire.MediaDownloadTicketAddExpiresAtUnix(builder, ticket.ExpiresAtUnix)
	builder.Finish(wire.MediaDownloadTicketEnd(builder))

	return builder.FinishedBytes()
}

func DecodeSearch(body []byte) (string, error) {
	var query string

	err := safely(func() {
		query = string(wire.GetRootAsSearch(body, 0).Query())
	})

	return query, err
}

func EncodeSearch(query string) []byte {
	builder := flatbuffers.NewBuilder(len(query) + 32)
	text := builder.CreateString(query)
	wire.SearchStart(builder)
	wire.SearchAddQuery(builder, text)
	builder.Finish(wire.SearchEnd(builder))

	return builder.FinishedBytes()
}

func DecodeSearchResults(body []byte) ([]SearchPerson, error) {
	var people []SearchPerson

	err := safely(func() {
		table := wire.GetRootAsSearchResults(body, 0)
		if table.PeopleLength() > maxSearchPeople {
			panic("too many people")
		}

		people = make([]SearchPerson, table.PeopleLength())

		var row wire.SearchPerson

		for index := range people {
			if !table.People(&row, index) {
				panic("person")
			}

			people[index] = SearchPerson{
				Subject:   string(row.Subject()),
				FirstName: string(row.FirstName()),
				LastName:  string(row.LastName()),
				Username:  string(row.Username()),
			}
		}
	})

	return people, err
}

func EncodeSearchResults(people []SearchPerson) []byte {
	builder := flatbuffers.NewBuilder(256)
	offsets := make([]flatbuffers.UOffsetT, len(people))

	for index, person := range people {
		subject := builder.CreateString(person.Subject)
		first := builder.CreateString(person.FirstName)
		last := builder.CreateString(person.LastName)
		username := builder.CreateString(person.Username)
		wire.SearchPersonStart(builder)
		wire.SearchPersonAddSubject(builder, subject)
		wire.SearchPersonAddFirstName(builder, first)
		wire.SearchPersonAddLastName(builder, last)
		wire.SearchPersonAddUsername(builder, username)
		offsets[index] = wire.SearchPersonEnd(builder)
	}

	wire.SearchResultsStartPeopleVector(builder, len(offsets))

	for index := len(offsets) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(offsets[index])
	}

	vector := builder.EndVector(len(offsets))
	wire.SearchResultsStart(builder)
	wire.SearchResultsAddPeople(builder, vector)
	builder.Finish(wire.SearchResultsEnd(builder))

	return builder.FinishedBytes()
}

func DecodePublicProfileGet(body []byte) (string, error) {
	var subject string

	err := safely(func() {
		subject = string(wire.GetRootAsPublicProfileGet(body, 0).Subject())
	})

	return subject, err
}

func EncodePublicProfileGet(subject string) []byte {
	builder := flatbuffers.NewBuilder(len(subject) + 32)
	text := builder.CreateString(subject)
	wire.PublicProfileGetStart(builder)
	wire.PublicProfileGetAddSubject(builder, text)
	builder.Finish(wire.PublicProfileGetEnd(builder))

	return builder.FinishedBytes()
}

func DecodePing(body []byte) (int64, error) {
	var id int64

	err := safely(func() {
		id = wire.GetRootAsPing(body, 0).PingId()
	})

	return id, err
}

func EncodePing(id int64) []byte {
	builder := flatbuffers.NewBuilder(32)
	wire.PingStart(builder)
	wire.PingAddPingId(builder, id)
	builder.Finish(wire.PingEnd(builder))

	return builder.FinishedBytes()
}

func DecodePong(body []byte) (int64, error) {
	var id int64

	err := safely(func() {
		id = wire.GetRootAsPong(body, 0).PingId()
	})

	return id, err
}

func EncodePong(id int64) []byte {
	builder := flatbuffers.NewBuilder(32)
	wire.PongStart(builder)
	wire.PongAddPingId(builder, id)
	builder.Finish(wire.PongEnd(builder))

	return builder.FinishedBytes()
}

func DecodeError(body []byte) (code int32, message string, err error) {
	err = safely(func() {
		table := wire.GetRootAsError(body, 0)
		code = table.Code()
		message = string(table.Message())
	})

	return code, message, err
}

func EncodeError(code int32, message string) []byte {
	builder := flatbuffers.NewBuilder(len(message) + 32)
	text := builder.CreateString(message)
	wire.ErrorStart(builder)
	wire.ErrorAddCode(builder, code)
	wire.ErrorAddMessage(builder, text)
	builder.Finish(wire.ErrorEnd(builder))

	return builder.FinishedBytes()
}

func DecodeProfileSet(body []byte) (ProfileFields, error) {
	var fields ProfileFields

	err := safely(func() {
		table := wire.GetRootAsProfileSet(body, 0)
		fields = ProfileFields{
			FirstName: string(table.FirstName()),
			LastName:  string(table.LastName()),
			Username:  string(table.Username()),
			Bio:       string(table.Bio()),
		}
	})

	return fields, err
}

func EncodeProfileSet(fields ProfileFields) []byte {
	builder := flatbuffers.NewBuilder(256)
	first := builder.CreateString(fields.FirstName)
	last := builder.CreateString(fields.LastName)
	username := builder.CreateString(fields.Username)
	bio := builder.CreateString(fields.Bio)
	wire.ProfileSetStart(builder)
	wire.ProfileSetAddFirstName(builder, first)
	wire.ProfileSetAddLastName(builder, last)
	wire.ProfileSetAddUsername(builder, username)
	wire.ProfileSetAddBio(builder, bio)
	builder.Finish(wire.ProfileSetEnd(builder))

	return builder.FinishedBytes()
}

func DecodeProfileData(body []byte) (ProfileData, error) {
	var data ProfileData

	err := safely(func() {
		table := wire.GetRootAsProfileData(body, 0)
		data = ProfileData{
			Subject: string(table.Subject()),
			Fields: ProfileFields{
				FirstName: string(table.FirstName()),
				LastName:  string(table.LastName()),
				Username:  string(table.Username()),
				Bio:       string(table.Bio()),
			},
			UpdatedAtUnix: table.UpdatedAtUnix(),
		}
	})

	return data, err
}

func EncodeProfileData(data ProfileData) []byte {
	builder := flatbuffers.NewBuilder(256)
	subject := builder.CreateString(data.Subject)
	first := builder.CreateString(data.Fields.FirstName)
	last := builder.CreateString(data.Fields.LastName)
	username := builder.CreateString(data.Fields.Username)
	bio := builder.CreateString(data.Fields.Bio)
	wire.ProfileDataStart(builder)
	wire.ProfileDataAddSubject(builder, subject)
	wire.ProfileDataAddFirstName(builder, first)
	wire.ProfileDataAddLastName(builder, last)
	wire.ProfileDataAddUsername(builder, username)
	wire.ProfileDataAddBio(builder, bio)
	wire.ProfileDataAddUpdatedAtUnix(builder, data.UpdatedAtUnix)
	builder.Finish(wire.ProfileDataEnd(builder))

	return builder.FinishedBytes()
}
