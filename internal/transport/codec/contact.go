package codec

import (
	flatbuffers "github.com/google/flatbuffers/go"

	"go.airlance.org/server/internal/transport/wire"
)

const (
	maxContacts   = 10000
	maxSyncPhones = 5000
)

const (
	KindContactsGet     = Kind(wire.MessageKindContactsGet)
	KindContactsData    = Kind(wire.MessageKindContactsData)
	KindContactAdd      = Kind(wire.MessageKindContactAdd)
	KindContactAddPhone = Kind(wire.MessageKindContactAddPhone)
	KindContactRemove   = Kind(wire.MessageKindContactRemove)
	KindContactsSync    = Kind(wire.MessageKindContactsSync)
	KindContactsChanged = Kind(wire.MessageKindContactsChanged)
)

type ContactData struct {
	Subject         string
	FirstName       string
	LastName        string
	Username        string
	PhotoMediaID    string
	VideoMediaID    string
	CustomFirstName string
	CustomLastName  string
	Phone           string
	FromDevice      bool
}

type ContactAdd struct {
	Target    string
	FirstName string
	LastName  string
}

func EncodeContactsData(contacts []ContactData) []byte {
	builder := flatbuffers.NewBuilder(1024)
	offsets := make([]flatbuffers.UOffsetT, len(contacts))

	for index, next := range contacts {
		subject := builder.CreateString(next.Subject)
		first := builder.CreateString(next.FirstName)
		last := builder.CreateString(next.LastName)
		username := builder.CreateString(next.Username)
		photo := builder.CreateString(next.PhotoMediaID)
		video := builder.CreateString(next.VideoMediaID)
		customFirst := builder.CreateString(next.CustomFirstName)
		customLast := builder.CreateString(next.CustomLastName)
		phone := builder.CreateString(next.Phone)
		wire.ContactDataStart(builder)
		wire.ContactDataAddSubject(builder, subject)
		wire.ContactDataAddFirstName(builder, first)
		wire.ContactDataAddLastName(builder, last)
		wire.ContactDataAddUsername(builder, username)
		wire.ContactDataAddPhotoMediaId(builder, photo)
		wire.ContactDataAddVideoMediaId(builder, video)
		wire.ContactDataAddCustomFirstName(builder, customFirst)
		wire.ContactDataAddCustomLastName(builder, customLast)
		wire.ContactDataAddPhone(builder, phone)
		wire.ContactDataAddFromDevice(builder, next.FromDevice)
		offsets[index] = wire.ContactDataEnd(builder)
	}

	wire.ContactsDataStartContactsVector(builder, len(offsets))

	for index := len(offsets) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(offsets[index])
	}

	vector := builder.EndVector(len(offsets))
	wire.ContactsDataStart(builder)
	wire.ContactsDataAddContacts(builder, vector)
	builder.Finish(wire.ContactsDataEnd(builder))

	return builder.FinishedBytes()
}

func DecodeContactsData(body []byte) ([]ContactData, error) {
	var contacts []ContactData

	err := safely(func() {
		table := wire.GetRootAsContactsData(body, 0)
		if table.ContactsLength() > maxContacts {
			panic("too many contacts")
		}

		var row wire.ContactData

		for index := range table.ContactsLength() {
			if !table.Contacts(&row, index) {
				panic("contact")
			}

			contacts = append(contacts, ContactData{
				Subject:         string(row.Subject()),
				FirstName:       string(row.FirstName()),
				LastName:        string(row.LastName()),
				Username:        string(row.Username()),
				PhotoMediaID:    string(row.PhotoMediaId()),
				VideoMediaID:    string(row.VideoMediaId()),
				CustomFirstName: string(row.CustomFirstName()),
				CustomLastName:  string(row.CustomLastName()),
				Phone:           string(row.Phone()),
				FromDevice:      row.FromDevice(),
			})
		}
	})

	return contacts, err
}

func EncodeContactAdd(next ContactAdd) []byte {
	builder := flatbuffers.NewBuilder(128)
	subject := builder.CreateString(next.Target)
	first := builder.CreateString(next.FirstName)
	last := builder.CreateString(next.LastName)
	wire.ContactAddStart(builder)
	wire.ContactAddAddSubject(builder, subject)
	wire.ContactAddAddCustomFirstName(builder, first)
	wire.ContactAddAddCustomLastName(builder, last)
	builder.Finish(wire.ContactAddEnd(builder))

	return builder.FinishedBytes()
}

func DecodeContactAdd(body []byte) (ContactAdd, error) {
	var next ContactAdd

	err := safely(func() {
		table := wire.GetRootAsContactAdd(body, 0)
		next = ContactAdd{
			Target:    string(table.Subject()),
			FirstName: string(table.CustomFirstName()),
			LastName:  string(table.CustomLastName()),
		}
	})

	return next, err
}

func EncodeContactAddPhone(next ContactAdd) []byte {
	builder := flatbuffers.NewBuilder(128)
	phone := builder.CreateString(next.Target)
	first := builder.CreateString(next.FirstName)
	last := builder.CreateString(next.LastName)
	wire.ContactAddPhoneStart(builder)
	wire.ContactAddPhoneAddPhone(builder, phone)
	wire.ContactAddPhoneAddCustomFirstName(builder, first)
	wire.ContactAddPhoneAddCustomLastName(builder, last)
	builder.Finish(wire.ContactAddPhoneEnd(builder))

	return builder.FinishedBytes()
}

func DecodeContactAddPhone(body []byte) (ContactAdd, error) {
	var next ContactAdd

	err := safely(func() {
		table := wire.GetRootAsContactAddPhone(body, 0)
		next = ContactAdd{
			Target:    string(table.Phone()),
			FirstName: string(table.CustomFirstName()),
			LastName:  string(table.CustomLastName()),
		}
	})

	return next, err
}

func EncodeContactRemove(subject string) []byte {
	builder := flatbuffers.NewBuilder(64)
	text := builder.CreateString(subject)
	wire.ContactRemoveStart(builder)
	wire.ContactRemoveAddSubject(builder, text)
	builder.Finish(wire.ContactRemoveEnd(builder))

	return builder.FinishedBytes()
}

func DecodeContactRemove(body []byte) (string, error) {
	var subject string

	err := safely(func() {
		subject = string(wire.GetRootAsContactRemove(body, 0).Subject())
	})

	return subject, err
}

func EncodeContactsSync(phones []string) []byte {
	builder := flatbuffers.NewBuilder(1024)
	offsets := make([]flatbuffers.UOffsetT, len(phones))

	for index, phone := range phones {
		offsets[index] = builder.CreateString(phone)
	}

	wire.ContactsSyncStartPhonesVector(builder, len(offsets))

	for index := len(offsets) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(offsets[index])
	}

	vector := builder.EndVector(len(offsets))
	wire.ContactsSyncStart(builder)
	wire.ContactsSyncAddPhones(builder, vector)
	builder.Finish(wire.ContactsSyncEnd(builder))

	return builder.FinishedBytes()
}

func DecodeContactsSync(body []byte) ([]string, error) {
	var phones []string

	err := safely(func() {
		table := wire.GetRootAsContactsSync(body, 0)
		if table.PhonesLength() > maxSyncPhones {
			panic("too many phones")
		}

		phones = make([]string, table.PhonesLength())
		for index := range phones {
			phones[index] = string(table.Phones(index))
		}
	})

	return phones, err
}

func EncodeContactsChanged() []byte {
	builder := flatbuffers.NewBuilder(16)
	wire.ContactsChangedStart(builder)
	builder.Finish(wire.ContactsChangedEnd(builder))

	return builder.FinishedBytes()
}
