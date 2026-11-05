package agent_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"go.airlance.org/server/internal/domain/agent"
	"go.airlance.org/server/internal/domain/message"
)

func valid() agent.Agent {
	return agent.Agent{
		Name: "Приветствие", ClientID: "c", ChatID: "chat", Trigger: agent.TriggerJoined, Text: "Привет, {name}!", Enabled: true,
	}
}

func TestAnAgentIsChecked(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		change func(*agent.Agent)
		want   error
	}{
		"no name":    {func(a *agent.Agent) { a.Name = " " }, agent.ErrInvalidName},
		"no client":  {func(a *agent.Agent) { a.ClientID = "" }, message.ErrInvalidClientID},
		"no text":    {func(a *agent.Agent) { a.Text = " " }, message.ErrInvalidText},
		"long text":  {func(a *agent.Agent) { a.Text = strings.Repeat("я", message.MaxTextLength+1) }, message.ErrInvalidText},
		"a trigger":  {func(a *agent.Agent) { a.Trigger = 9 }, agent.ErrInvalidTrigger},
		"no keyword": {func(a *agent.Agent) { a.Trigger = agent.TriggerKeyword }, agent.ErrInvalidKeyword},
		"an hour":    {func(a *agent.Agent) { a.Trigger, a.Hour, a.Weekdays = agent.TriggerSchedule, 24, 1 }, agent.ErrInvalidTime},
		"no days":    {func(a *agent.Agent) { a.Trigger, a.Weekdays = agent.TriggerSchedule, 0 }, agent.ErrInvalidTime},
		"a zone": {func(a *agent.Agent) {
			a.Trigger, a.Weekdays, a.Timezone = agent.TriggerSchedule, 1, "Mars/Olympus"
		}, agent.ErrInvalidZone},
	}

	for name, next := range cases {
		draft := valid()
		next.change(&draft)

		if _, err := draft.Checked(); !errors.Is(err, next.want) {
			t.Errorf("%s: got %v", name, err)
		}
	}

	hook := valid()
	hook.Trigger, hook.Text = agent.TriggerWebhook, ""

	if _, err := hook.Checked(); err != nil {
		t.Fatalf("a webhook may pass on its text: %v", err)
	}
}

func TestTheScheduleHonoursDaysAndTheLocalClock(t *testing.T) {
	t.Parallel()

	zone, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		t.Fatal(err)
	}

	monday := 1 << int(time.Monday)
	weekly := agent.Agent{Trigger: agent.TriggerSchedule, Hour: 9, Minute: 30, Weekdays: monday, Timezone: "Europe/Kyiv"}
	saturday := time.Date(2026, 3, 28, 12, 0, 0, 0, zone)

	next := weekly.NextRun(saturday)
	if next.In(zone).Weekday() != time.Monday || next.In(zone).Hour() != 9 || next.In(zone).Minute() != 30 {
		t.Fatalf("got %v", next.In(zone))
	}

	daily := weekly
	daily.Weekdays = agent.AllWeekdays
	early := time.Date(2026, 3, 28, 9, 0, 0, 0, zone)

	if got := daily.NextRun(early); !got.Equal(time.Date(2026, 3, 28, 9, 30, 0, 0, zone)) {
		t.Fatalf("today is still ahead: %v", got)
	}

	exact := time.Date(2026, 3, 28, 9, 30, 0, 0, zone)
	if got := daily.NextRun(exact); !got.After(exact) {
		t.Fatalf("a run repeated itself: %v", got)
	}
}

func TestEventsReachOnlyTheirAgents(t *testing.T) {
	t.Parallel()

	keyword := valid()
	keyword.Trigger, keyword.Keyword = agent.TriggerKeyword, "Цена"

	if !keyword.Matches(agent.Event{Trigger: agent.TriggerKeyword, ChatID: "chat", Text: "какая цена?"}) {
		t.Fatal("a keyword is not case sensitive")
	}

	if keyword.Matches(agent.Event{Trigger: agent.TriggerKeyword, ChatID: "chat", Text: "привет"}) {
		t.Fatal("no keyword, no reply")
	}

	if keyword.Matches(agent.Event{Trigger: agent.TriggerKeyword, ChatID: "other", Text: "цена"}) {
		t.Fatal("another chat")
	}

	keyword.Enabled = false
	if keyword.Matches(agent.Event{Trigger: agent.TriggerKeyword, ChatID: "chat", Text: "цена"}) {
		t.Fatal("a stopped agent")
	}

	hook := valid()
	hook.ID, hook.Trigger = "a1", agent.TriggerWebhook

	if hook.Matches(agent.Event{Trigger: agent.TriggerWebhook, ChatID: "chat", AgentID: "a2"}) {
		t.Fatal("another agent's webhook")
	}
}

func TestTheTextIsFilledIn(t *testing.T) {
	t.Parallel()

	greeting := valid()
	if got := greeting.Render("Анна", ""); got != "Привет, Анна!" {
		t.Fatal(got)
	}

	relay := valid()
	relay.Text = ""

	if got := relay.Render("", " сборка готова "); got != "сборка готова" {
		t.Fatal(got)
	}

	wrapped := valid()
	wrapped.Text = "CI: {text}"

	if got := wrapped.Render("", strings.Repeat("я", message.MaxTextLength+50)); len([]rune(got)) != message.MaxTextLength {
		t.Fatalf("got %d", len([]rune(got)))
	}
}
