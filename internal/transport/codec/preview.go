package codec

import (
	flatbuffers "github.com/google/flatbuffers/go"

	"go.airlance.org/server/internal/transport/wire"
)

const (
	KindLinkPreviewGet  = Kind(wire.MessageKindLinkPreviewGet)
	KindLinkPreviewData = Kind(wire.MessageKindLinkPreviewData)
)

type LinkPreviewData struct {
	Found        bool
	URL          string
	Site         string
	Title        string
	Summary      string
	PhotoMediaID string
	Width        int32
	Height       int32
}

func DecodeLinkPreviewGet(body []byte) (string, error) {
	var link string

	err := safely(func() {
		link = string(wire.GetRootAsLinkPreviewGet(body, 0).Url())
	})

	return link, err
}

func EncodeLinkPreviewData(data LinkPreviewData) []byte {
	builder := flatbuffers.NewBuilder(512)
	link := builder.CreateString(data.URL)
	site := builder.CreateString(data.Site)
	title := builder.CreateString(data.Title)
	summary := builder.CreateString(data.Summary)
	photo := builder.CreateString(data.PhotoMediaID)
	wire.LinkPreviewDataStart(builder)
	wire.LinkPreviewDataAddFound(builder, data.Found)
	wire.LinkPreviewDataAddUrl(builder, link)
	wire.LinkPreviewDataAddSite(builder, site)
	wire.LinkPreviewDataAddTitle(builder, title)
	wire.LinkPreviewDataAddSummary(builder, summary)
	wire.LinkPreviewDataAddPhotoMediaId(builder, photo)
	wire.LinkPreviewDataAddWidth(builder, data.Width)
	wire.LinkPreviewDataAddHeight(builder, data.Height)
	builder.Finish(wire.LinkPreviewDataEnd(builder))

	return builder.FinishedBytes()
}
