package codec

import (
	flatbuffers "github.com/google/flatbuffers/go"

	"go.airlance.org/server/internal/transport/wire"
)

const (
	KindPrivacyGet  = Kind(wire.MessageKindPrivacyGet)
	KindPrivacySet  = Kind(wire.MessageKindPrivacySet)
	KindPrivacyData = Kind(wire.MessageKindPrivacyData)
	KindBlocksGet   = Kind(wire.MessageKindBlocksGet)
	KindBlocksData  = Kind(wire.MessageKindBlocksData)
	KindBlockSet    = Kind(wire.MessageKindBlockSet)
)

type PrivacySettings struct {
	Phone    int32
	Photos   int32
	Bio      int32
	Messages int32
	Search   int32
}

func DecodePrivacySet(body []byte) (PrivacySettings, error) {
	var settings PrivacySettings

	err := safely(func() {
		table := wire.GetRootAsPrivacySet(body, 0)
		settings = PrivacySettings{
			Phone:    table.Phone(),
			Photos:   table.Photos(),
			Bio:      table.Bio(),
			Messages: table.Messages(),
			Search:   table.Search(),
		}
	})

	return settings, err
}

func EncodePrivacySet(settings PrivacySettings) []byte {
	builder := flatbuffers.NewBuilder(64)
	wire.PrivacySetStart(builder)
	wire.PrivacySetAddPhone(builder, settings.Phone)
	wire.PrivacySetAddPhotos(builder, settings.Photos)
	wire.PrivacySetAddBio(builder, settings.Bio)
	wire.PrivacySetAddMessages(builder, settings.Messages)
	wire.PrivacySetAddSearch(builder, settings.Search)
	builder.Finish(wire.PrivacySetEnd(builder))

	return builder.FinishedBytes()
}

func DecodePrivacyData(body []byte) (PrivacySettings, error) {
	var settings PrivacySettings

	err := safely(func() {
		table := wire.GetRootAsPrivacyData(body, 0)
		settings = PrivacySettings{
			Phone:    table.Phone(),
			Photos:   table.Photos(),
			Bio:      table.Bio(),
			Messages: table.Messages(),
			Search:   table.Search(),
		}
	})

	return settings, err
}

func EncodePrivacyData(settings PrivacySettings) []byte {
	builder := flatbuffers.NewBuilder(64)
	wire.PrivacyDataStart(builder)
	wire.PrivacyDataAddPhone(builder, settings.Phone)
	wire.PrivacyDataAddPhotos(builder, settings.Photos)
	wire.PrivacyDataAddBio(builder, settings.Bio)
	wire.PrivacyDataAddMessages(builder, settings.Messages)
	wire.PrivacyDataAddSearch(builder, settings.Search)
	builder.Finish(wire.PrivacyDataEnd(builder))

	return builder.FinishedBytes()
}

func DecodeBlockSet(body []byte) (subject string, blocked bool, err error) {
	err = safely(func() {
		table := wire.GetRootAsBlockSet(body, 0)
		subject = string(table.Subject())
		blocked = table.Blocked()
	})

	return subject, blocked, err
}

func EncodeBlockSet(subject string, blocked bool) []byte {
	builder := flatbuffers.NewBuilder(64)
	text := builder.CreateString(subject)
	wire.BlockSetStart(builder)
	wire.BlockSetAddSubject(builder, text)
	wire.BlockSetAddBlocked(builder, blocked)
	builder.Finish(wire.BlockSetEnd(builder))

	return builder.FinishedBytes()
}

func EncodeBlocksData(people []SearchPerson) []byte {
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

	wire.BlocksDataStartPeopleVector(builder, len(offsets))

	for index := len(offsets) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(offsets[index])
	}

	vector := builder.EndVector(len(offsets))
	wire.BlocksDataStart(builder)
	wire.BlocksDataAddPeople(builder, vector)
	builder.Finish(wire.BlocksDataEnd(builder))

	return builder.FinishedBytes()
}

func DecodeBlocksData(body []byte) ([]SearchPerson, error) {
	var people []SearchPerson

	err := safely(func() {
		table := wire.GetRootAsBlocksData(body, 0)
		if table.PeopleLength() > maxSearchPeople*maxBlockPages {
			panic("too many people")
		}

		var row wire.SearchPerson

		for index := range table.PeopleLength() {
			if !table.People(&row, index) {
				panic("person")
			}

			people = append(people, SearchPerson{
				Subject:   string(row.Subject()),
				FirstName: string(row.FirstName()),
				LastName:  string(row.LastName()),
				Username:  string(row.Username()),
			})
		}
	})

	return people, err
}

const maxBlockPages = 40
