package codec

import (
	flatbuffers "github.com/google/flatbuffers/go"

	"go.airlance.org/server/internal/transport/wire"
)

const (
	KindScheduledGet     = Kind(wire.MessageKindScheduledGet)
	KindScheduledAdd     = Kind(wire.MessageKindScheduledAdd)
	KindScheduledEdit    = Kind(wire.MessageKindScheduledEdit)
	KindScheduledCancel  = Kind(wire.MessageKindScheduledCancel)
	KindScheduledSendNow = Kind(wire.MessageKindScheduledSendNow)
	KindScheduledData    = Kind(wire.MessageKindScheduledData)
	KindScheduledChanged = Kind(wire.MessageKindScheduledChanged)
	KindAgentsGet        = Kind(wire.MessageKindAgentsGet)
	KindAgentSave        = Kind(wire.MessageKindAgentSave)
	KindAgentDelete      = Kind(wire.MessageKindAgentDelete)
	KindAgentsData       = Kind(wire.MessageKindAgentsData)
	KindAgentRunsGet     = Kind(wire.MessageKindAgentRunsGet)
	KindAgentRunsData    = Kind(wire.MessageKindAgentRunsData)
)

type ScheduledAdd struct {
	ClientID string
	ChatID   string
	Text     string
	SendAtMs int64
	Repeat   int32
	Timezone string
}

type ScheduledEdit struct {
	ID       string
	Text     string
	SendAtMs int64
}

type ScheduledItem struct {
	ID       string
	ChatID   string
	ClientID string
	Text     string
	SendAtMs int64
	Repeat   int32
	Failure  string
}

type AgentData struct {
	ID        string
	ClientID  string
	Name      string
	ChatID    string
	ChatTitle string
	Trigger   int32
	Keyword   string
	Hour      int32
	Minute    int32
	Weekdays  int32
	Timezone  string
	Text      string
	Enabled   bool
	NextRunMs int64
}

type AgentSave struct {
	Agent  AgentData
	Rotate bool
}

type AgentRunData struct {
	Status  int32
	Detail  string
	RanAtMs int64
}

func DecodeScheduledAdd(body []byte) (ScheduledAdd, error) {
	var add ScheduledAdd

	err := safely(func() {
		table := wire.GetRootAsScheduledAdd(body, 0)
		add = ScheduledAdd{
			ClientID: string(table.ClientId()),
			ChatID:   string(table.ChatId()),
			Text:     string(table.Text()),
			SendAtMs: table.SendAtUnixMs(),
			Repeat:   table.RepeatRule(),
			Timezone: string(table.Timezone()),
		}
	})

	return add, err
}

func DecodeScheduledEdit(body []byte) (ScheduledEdit, error) {
	var edit ScheduledEdit

	err := safely(func() {
		table := wire.GetRootAsScheduledEdit(body, 0)
		edit = ScheduledEdit{ID: string(table.Id()), Text: string(table.Text()), SendAtMs: table.SendAtUnixMs()}
	})

	return edit, err
}

func DecodeScheduledCancel(body []byte) (string, error) {
	var id string

	err := safely(func() {
		id = string(wire.GetRootAsScheduledCancel(body, 0).Id())
	})

	return id, err
}

func DecodeScheduledSendNow(body []byte) (string, error) {
	var id string

	err := safely(func() {
		id = string(wire.GetRootAsScheduledSendNow(body, 0).Id())
	})

	return id, err
}

func EncodeScheduledData(items []ScheduledItem) []byte {
	builder := flatbuffers.NewBuilder(1024)
	offsets := make([]flatbuffers.UOffsetT, len(items))

	for index, next := range items {
		id := builder.CreateString(next.ID)
		chat := builder.CreateString(next.ChatID)
		client := builder.CreateString(next.ClientID)
		text := builder.CreateString(next.Text)
		failure := builder.CreateString(next.Failure)
		wire.ScheduledItemStart(builder)
		wire.ScheduledItemAddId(builder, id)
		wire.ScheduledItemAddChatId(builder, chat)
		wire.ScheduledItemAddClientId(builder, client)
		wire.ScheduledItemAddText(builder, text)
		wire.ScheduledItemAddSendAtUnixMs(builder, next.SendAtMs)
		wire.ScheduledItemAddRepeatRule(builder, next.Repeat)
		wire.ScheduledItemAddFailure(builder, failure)
		offsets[index] = wire.ScheduledItemEnd(builder)
	}

	wire.ScheduledDataStartItemsVector(builder, len(offsets))

	for index := len(offsets) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(offsets[index])
	}

	vector := builder.EndVector(len(offsets))
	wire.ScheduledDataStart(builder)
	wire.ScheduledDataAddItems(builder, vector)
	builder.Finish(wire.ScheduledDataEnd(builder))

	return builder.FinishedBytes()
}

func DecodeScheduledData(body []byte) ([]ScheduledItem, error) {
	var items []ScheduledItem

	err := safely(func() {
		table := wire.GetRootAsScheduledData(body, 0)
		if table.ItemsLength() > maxListItems {
			panic("too many items")
		}

		items = make([]ScheduledItem, table.ItemsLength())

		var row wire.ScheduledItem

		for index := range items {
			if !table.Items(&row, index) {
				panic("item")
			}

			items[index] = ScheduledItem{
				ID:       string(row.Id()),
				ChatID:   string(row.ChatId()),
				ClientID: string(row.ClientId()),
				Text:     string(row.Text()),
				SendAtMs: row.SendAtUnixMs(),
				Repeat:   row.RepeatRule(),
				Failure:  string(row.Failure()),
			}
		}
	})

	return items, err
}

func EncodeScheduledChanged() []byte {
	builder := flatbuffers.NewBuilder(16)
	wire.ScheduledChangedStart(builder)
	builder.Finish(wire.ScheduledChangedEnd(builder))

	return builder.FinishedBytes()
}

func DecodeAgentSave(body []byte) (AgentSave, error) {
	var save AgentSave

	err := safely(func() {
		table := wire.GetRootAsAgentSave(body, 0)
		save = AgentSave{Rotate: table.RotateWebhook(), Agent: AgentData{
			ID:       string(table.Id()),
			ClientID: string(table.ClientId()),
			Name:     string(table.Name()),
			ChatID:   string(table.ChatId()),
			Trigger:  table.TriggerKind(),
			Keyword:  string(table.Keyword()),
			Hour:     table.Hour(),
			Minute:   table.Minute(),
			Weekdays: table.Weekdays(),
			Timezone: string(table.Timezone()),
			Text:     string(table.Text()),
			Enabled:  table.Enabled(),
		}}
	})

	return save, err
}

func DecodeAgentDelete(body []byte) (string, error) {
	var id string

	err := safely(func() {
		id = string(wire.GetRootAsAgentDelete(body, 0).Id())
	})

	return id, err
}

func DecodeAgentRunsGet(body []byte) (string, error) {
	var id string

	err := safely(func() {
		id = string(wire.GetRootAsAgentRunsGet(body, 0).Id())
	})

	return id, err
}

func buildAgent(builder *flatbuffers.Builder, next AgentData) flatbuffers.UOffsetT {
	id := builder.CreateString(next.ID)
	client := builder.CreateString(next.ClientID)
	name := builder.CreateString(next.Name)
	chat := builder.CreateString(next.ChatID)
	title := builder.CreateString(next.ChatTitle)
	keyword := builder.CreateString(next.Keyword)
	zone := builder.CreateString(next.Timezone)
	text := builder.CreateString(next.Text)
	wire.AgentDataStart(builder)
	wire.AgentDataAddId(builder, id)
	wire.AgentDataAddClientId(builder, client)
	wire.AgentDataAddName(builder, name)
	wire.AgentDataAddChatId(builder, chat)
	wire.AgentDataAddChatTitle(builder, title)
	wire.AgentDataAddTriggerKind(builder, next.Trigger)
	wire.AgentDataAddKeyword(builder, keyword)
	wire.AgentDataAddHour(builder, next.Hour)
	wire.AgentDataAddMinute(builder, next.Minute)
	wire.AgentDataAddWeekdays(builder, next.Weekdays)
	wire.AgentDataAddTimezone(builder, zone)
	wire.AgentDataAddText(builder, text)
	wire.AgentDataAddEnabled(builder, next.Enabled)
	wire.AgentDataAddNextRunUnixMs(builder, next.NextRunMs)

	return wire.AgentDataEnd(builder)
}

func EncodeAgentsData(agents []AgentData, webhookToken string) []byte {
	builder := flatbuffers.NewBuilder(1024)
	offsets := make([]flatbuffers.UOffsetT, len(agents))

	for index, next := range agents {
		offsets[index] = buildAgent(builder, next)
	}

	token := builder.CreateString(webhookToken)
	wire.AgentsDataStartAgentsVector(builder, len(offsets))

	for index := len(offsets) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(offsets[index])
	}

	vector := builder.EndVector(len(offsets))
	wire.AgentsDataStart(builder)
	wire.AgentsDataAddAgents(builder, vector)
	wire.AgentsDataAddWebhookToken(builder, token)
	builder.Finish(wire.AgentsDataEnd(builder))

	return builder.FinishedBytes()
}

func EncodeAgentRunsData(runs []AgentRunData) []byte {
	builder := flatbuffers.NewBuilder(1024)
	offsets := make([]flatbuffers.UOffsetT, len(runs))

	for index, next := range runs {
		detail := builder.CreateString(next.Detail)
		wire.AgentRunDataStart(builder)
		wire.AgentRunDataAddStatus(builder, next.Status)
		wire.AgentRunDataAddDetail(builder, detail)
		wire.AgentRunDataAddRanAtUnixMs(builder, next.RanAtMs)
		offsets[index] = wire.AgentRunDataEnd(builder)
	}

	wire.AgentRunsDataStartRunsVector(builder, len(offsets))

	for index := len(offsets) - 1; index >= 0; index-- {
		builder.PrependUOffsetT(offsets[index])
	}

	vector := builder.EndVector(len(offsets))
	wire.AgentRunsDataStart(builder)
	wire.AgentRunsDataAddRuns(builder, vector)
	builder.Finish(wire.AgentRunsDataEnd(builder))

	return builder.FinishedBytes()
}
