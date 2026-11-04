package codec

import (
	"errors"
	"fmt"

	flatbuffers "github.com/google/flatbuffers/go"

	"go.airlance.org/server/internal/transport/wire"
)

const fileIdentifier = "ALNC"

const MaxFrameBytes = 4 << 20

var ErrInvalid = errors.New("invalid frame")

type Kind uint16

const (
	KindPing        = Kind(wire.MessageKindPing)
	KindPong        = Kind(wire.MessageKindPong)
	KindError       = Kind(wire.MessageKindError)
	KindProfileGet  = Kind(wire.MessageKindProfileGet)
	KindProfileSet  = Kind(wire.MessageKindProfileSet)
	KindProfileData = Kind(wire.MessageKindProfileData)
)

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
