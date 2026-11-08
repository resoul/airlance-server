package codec

import (
	flatbuffers "github.com/google/flatbuffers/go"

	"go.airlance.org/server/internal/transport/wire"
)

const (
	KindSettingsGet   = Kind(wire.MessageKindSettingsGet)
	KindSettingsPut   = Kind(wire.MessageKindSettingsPut)
	KindSettingsData  = Kind(wire.MessageKindSettingsData)
	KindWallpapersGet = Kind(wire.MessageKindWallpapersGet)
	KindWallpapers    = Kind(wire.MessageKindWallpapersData)

	maxWallpapers = 200
	maxSettings   = 70 * 1024
)

type SettingsData struct {
	Key     string
	Version int64
	Data    []byte
}

type Wallpaper struct {
	ID      string
	Name    string
	MediaID string
}

func DecodeSettingsGet(body []byte) (string, error) {
	var key string

	err := safely(func() {
		key = string(wire.GetRootAsSettingsGet(body, 0).Key())
	})

	return key, err
}

func EncodeSettingsGet(key string) []byte {
	builder := flatbuffers.NewBuilder(64)
	name := builder.CreateString(key)
	wire.SettingsGetStart(builder)
	wire.SettingsGetAddKey(builder, name)
	builder.Finish(wire.SettingsGetEnd(builder))

	return builder.FinishedBytes()
}

func DecodeSettingsPut(body []byte) (string, []byte, error) {
	var (
		key  string
		data []byte
	)

	err := safely(func() {
		table := wire.GetRootAsSettingsPut(body, 0)
		if table.DataLength() > maxSettings {
			panic("settings are too big")
		}

		key, data = string(table.Key()), append([]byte(nil), table.DataBytes()...)
	})

	return key, data, err
}

func EncodeSettingsPut(key string, data []byte) []byte {
	builder := flatbuffers.NewBuilder(len(data) + 64)
	name := builder.CreateString(key)
	payload := builder.CreateByteVector(data)
	wire.SettingsPutStart(builder)
	wire.SettingsPutAddKey(builder, name)
	wire.SettingsPutAddData(builder, payload)
	builder.Finish(wire.SettingsPutEnd(builder))

	return builder.FinishedBytes()
}

func EncodeSettingsData(next SettingsData) []byte {
	builder := flatbuffers.NewBuilder(len(next.Data) + 64)
	name := builder.CreateString(next.Key)
	payload := builder.CreateByteVector(next.Data)
	wire.SettingsDataStart(builder)
	wire.SettingsDataAddKey(builder, name)
	wire.SettingsDataAddVersion(builder, next.Version)
	wire.SettingsDataAddData(builder, payload)
	builder.Finish(wire.SettingsDataEnd(builder))

	return builder.FinishedBytes()
}

func DecodeSettingsData(body []byte) (SettingsData, error) {
	var next SettingsData

	err := safely(func() {
		table := wire.GetRootAsSettingsData(body, 0)
		if table.DataLength() > maxSettings {
			panic("settings are too big")
		}

		next = SettingsData{
			Key: string(table.Key()), Version: table.Version(), Data: append([]byte(nil), table.DataBytes()...),
		}
	})

	return next, err
}

func EncodeWallpapersGet() []byte {
	builder := flatbuffers.NewBuilder(16)
	wire.WallpapersGetStart(builder)
	builder.Finish(wire.WallpapersGetEnd(builder))

	return builder.FinishedBytes()
}

func EncodeWallpapers(items []Wallpaper) []byte {
	builder := flatbuffers.NewBuilder(256)
	offsets := make([]flatbuffers.UOffsetT, len(items))

	for index, next := range items {
		id := builder.CreateString(next.ID)
		name := builder.CreateString(next.Name)
		media := builder.CreateString(next.MediaID)
		wire.WallpaperDataStart(builder)
		wire.WallpaperDataAddId(builder, id)
		wire.WallpaperDataAddName(builder, name)
		wire.WallpaperDataAddMediaId(builder, media)
		offsets[index] = wire.WallpaperDataEnd(builder)
	}

	wire.WallpapersDataStartItemsVector(builder, len(offsets))

	for index := len(offsets) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(offsets[index])
	}

	vector := builder.EndVector(len(offsets))
	wire.WallpapersDataStart(builder)
	wire.WallpapersDataAddItems(builder, vector)
	builder.Finish(wire.WallpapersDataEnd(builder))

	return builder.FinishedBytes()
}

func DecodeWallpapers(body []byte) ([]Wallpaper, error) {
	var items []Wallpaper

	err := safely(func() {
		table := wire.GetRootAsWallpapersData(body, 0)
		if table.ItemsLength() > maxWallpapers {
			panic("too many wallpapers")
		}

		var row wire.WallpaperData

		for index := range table.ItemsLength() {
			if !table.Items(&row, index) {
				panic("wallpaper")
			}

			items = append(items, Wallpaper{ID: string(row.Id()), Name: string(row.Name()), MediaID: string(row.MediaId())})
		}
	})

	return items, err
}
