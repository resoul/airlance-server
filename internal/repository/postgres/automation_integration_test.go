//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"go.airlance.org/server/internal/domain/agent"
	domain "go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/scheduled"
	"go.airlance.org/server/internal/domain/update"
	"go.airlance.org/server/internal/repository/postgres"
	agents "go.airlance.org/server/internal/usecase/agent"
	usecase "go.airlance.org/server/internal/usecase/message"
	schedule "go.airlance.org/server/internal/usecase/scheduled"
)

type stage struct {
	ctx       context.Context
	pool      *pgxpool.Pool
	messages  *usecase.Service
	scheduler *schedule.Service
	agents    *agents.Service
	clock     *time.Time
	errors    []error
}

func newStage(t *testing.T) *stage {
	t.Helper()

	ctx, pool := openGroups(t)

	_, err := pool.Exec(ctx, "TRUNCATE scheduled_messages, agents, agent_events, agent_runs")
	if err != nil {
		t.Fatal(err)
	}

	clock := time.Now()
	now := func() time.Time {
		return clock
	}
	result := &stage{ctx: ctx, pool: pool, clock: &clock}
	messages := messengerAt(pool, now)
	result.messages = messages
	onError := func(err error) {
		result.errors = append(result.errors, err)
	}
	result.scheduler = schedule.New(postgres.NewScheduled(pool), messages, silentNotifier{}, now, onError)
	result.agents = agents.New(postgres.NewAgents(pool), messages, now, onError)

	return result
}

func (s *stage) history(t *testing.T, who, chatID string) []domain.Message {
	t.Helper()

	found, err := s.messages.History(s.ctx, person(who), chatID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}

	return found
}

func (s *stage) group(t *testing.T, owner string, people ...string) domain.Chat {
	t.Helper()

	chat, err := s.messages.Create(s.ctx, person(owner), usecase.NewChat{
		ClientID: "c-" + time.Now().String(), Kind: domain.KindGroup, Title: "Группа", People: people,
	})
	if err != nil {
		t.Fatal(err)
	}

	return chat
}

func TestAScheduledMessageWaitsThenGoesOutOnce(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "anna", "petro")
	chat := s.group(t, "anna", "petro")
	at := s.clock.Add(time.Hour)

	items, err := s.scheduler.Add(s.ctx, person("anna"), schedule.Draft{ClientID: "l1", ChatID: chat.ID, Text: "позже", SendAt: at})
	if err != nil || len(items) != 1 {
		t.Fatalf("got %+v, %v", items, err)
	}

	if _, err := s.scheduler.Add(s.ctx, person("anna"), schedule.Draft{ClientID: "l1", ChatID: chat.ID, Text: "позже", SendAt: at}); err != nil {
		t.Fatalf("a repeat: %v", err)
	}

	if err := s.scheduler.Drain(s.ctx); err != nil {
		t.Fatal(err)
	}

	if got := s.history(t, "petro", chat.ID); len(got) != 0 {
		t.Fatalf("went out early: %+v", got)
	}

	*s.clock = at.Add(time.Second)
	if err := s.scheduler.Drain(s.ctx); err != nil {
		t.Fatal(err)
	}

	got := s.history(t, "petro", chat.ID)
	if len(got) != 1 || got[0].Text != "позже" || got[0].ClientID != "l1" || got[0].Sender != "anna" {
		t.Fatalf("got %+v", got)
	}

	if err := s.scheduler.Drain(s.ctx); err != nil {
		t.Fatal(err)
	}

	left, err := s.scheduler.List(s.ctx, person("anna"))
	if err != nil || len(left) != 0 || len(s.history(t, "petro", chat.ID)) != 1 {
		t.Fatalf("left %+v, %v", left, err)
	}
}

func TestSomeoneElsesScheduledMessageIsOutOfReach(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "anna", "petro", "stranger")
	chat := s.group(t, "anna", "petro")
	at := s.clock.Add(time.Hour)

	items, err := s.scheduler.Add(s.ctx, person("anna"), schedule.Draft{ClientID: "l1", ChatID: chat.ID, Text: "моё", SendAt: at})
	if err != nil {
		t.Fatal(err)
	}

	id := items[0].ID

	if _, err := s.scheduler.Cancel(s.ctx, person("petro"), id); !errors.Is(err, scheduled.ErrNotFound) {
		t.Fatalf("cancel: %v", err)
	}

	if _, err := s.scheduler.Edit(s.ctx, person("petro"), id, "чужое", at); !errors.Is(err, scheduled.ErrNotFound) {
		t.Fatalf("edit: %v", err)
	}

	if _, err := s.scheduler.SendNow(s.ctx, person("petro"), id); !errors.Is(err, scheduled.ErrNotFound) {
		t.Fatalf("send now: %v", err)
	}

	_, err = s.scheduler.Add(s.ctx, person("stranger"), schedule.Draft{ClientID: "s", ChatID: chat.ID, Text: "эй", SendAt: at})
	if !errors.Is(err, domain.ErrChatNotFound) {
		t.Fatalf("a stranger scheduled: %v", err)
	}

	if _, err := s.scheduler.SendNow(s.ctx, person("anna"), id); err != nil {
		t.Fatal(err)
	}

	if err := s.scheduler.Drain(s.ctx); err != nil {
		t.Fatal(err)
	}

	if got := s.history(t, "petro", chat.ID); len(got) != 1 {
		t.Fatalf("send now did nothing: %+v", got)
	}
}

func TestLosingAccessMarksAScheduledMessageFailed(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "owner", "anna")
	chat := s.group(t, "owner", "anna")
	at := s.clock.Add(time.Hour)

	if _, err := s.scheduler.Add(s.ctx, person("anna"), schedule.Draft{ClientID: "l", ChatID: chat.ID, Text: "текст", SendAt: at}); err != nil {
		t.Fatal(err)
	}

	if err := s.messages.Leave(s.ctx, person("anna"), chat.ID); err != nil {
		t.Fatal(err)
	}

	*s.clock = at.Add(time.Second)
	if err := s.scheduler.Drain(s.ctx); err != nil {
		t.Fatal(err)
	}

	items, err := s.scheduler.List(s.ctx, person("anna"))
	if err != nil || len(items) != 1 || items[0].Failure != "no_access" {
		t.Fatalf("got %+v, %v", items, err)
	}

	if got := s.history(t, "owner", chat.ID); len(got) != 0 {
		t.Fatalf("it went out: %+v", got)
	}

	journal, err := postgres.NewUpdates(s.pool).After(s.ctx, "anna", 0, 100)
	if err != nil || journal[len(journal)-1].Kind != update.KindScheduled {
		t.Fatalf("no news for the sender: %+v, %v", journal, err)
	}
}

func TestARepeatingMessageComesBackForTheNextTime(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "anna")
	chat := s.group(t, "anna")
	at := s.clock.Add(time.Hour)

	_, err := s.scheduler.Add(s.ctx, person("anna"), schedule.Draft{
		ClientID: "l", ChatID: chat.ID, Text: "каждый день", SendAt: at, Repeat: scheduled.RepeatDaily, Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}

	for day := range 2 {
		*s.clock = at.Add(time.Duration(day)*24*time.Hour + time.Second)
		if err := s.scheduler.Drain(s.ctx); err != nil {
			t.Fatal(err)
		}
	}

	got := s.history(t, "anna", chat.ID)
	if len(got) != 2 || got[0].ClientID == got[1].ClientID {
		t.Fatalf("got %+v", got)
	}

	items, err := s.scheduler.List(s.ctx, person("anna"))
	if err != nil || len(items) != 1 || !items[0].SendAt.Equal(at.Add(48*time.Hour)) {
		t.Fatalf("got %+v, %v", items, err)
	}
}

func saveAgent(t *testing.T, s *stage, owner string, draft agent.Agent, rotate bool) agents.Saved {
	t.Helper()

	draft.ClientID, draft.Enabled = "a-"+time.Now().String(), true

	saved, err := s.agents.Save(s.ctx, person(owner), draft, rotate)
	if err != nil {
		t.Fatal(err)
	}

	return saved
}

func TestOnlyAnAdminCanPutAnAgentInAChat(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "owner", "member", "stranger")
	chat := s.group(t, "owner", "member")
	draft := agent.Agent{ClientID: "c", Name: "Бот", ChatID: chat.ID, Trigger: agent.TriggerJoined, Text: "Привет"}

	if _, err := s.agents.Save(s.ctx, person("member"), draft, false); !errors.Is(err, domain.ErrNotAllowed) {
		t.Fatalf("a member: %v", err)
	}

	if _, err := s.agents.Save(s.ctx, person("stranger"), draft, false); !errors.Is(err, domain.ErrChatNotFound) {
		t.Fatalf("a stranger: %v", err)
	}

	saved := saveAgent(t, s, "owner", draft, false)
	draft.ID = saved.Agents[0].ID
	draft.ClientID = "other"

	if _, err := s.agents.Runs(s.ctx, person("member"), draft.ID); !errors.Is(err, agent.ErrNotFound) {
		t.Fatalf("someone else's log: %v", err)
	}

	if _, err := s.agents.Delete(s.ctx, person("member"), draft.ID); !errors.Is(err, agent.ErrNotFound) {
		t.Fatalf("someone else's delete: %v", err)
	}
}

func TestAnAgentGreetsANewcomerAndSaysItIsAnAgent(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "owner", "petro")
	chat := s.group(t, "owner")
	saved := saveAgent(t, s, "owner", agent.Agent{
		Name: "Приветствие", ChatID: chat.ID, Trigger: agent.TriggerJoined, Text: "Привет, {name}!",
	}, false)

	if _, err := s.messages.Join(s.ctx, person("petro"), chat.InviteToken); err != nil {
		t.Fatal(err)
	}

	if err := s.agents.Drain(s.ctx); err != nil {
		t.Fatal(err)
	}

	got := s.history(t, "petro", chat.ID)
	if len(got) != 1 || got[0].Text != "Привет, petro!" || got[0].AgentName != "Приветствие" || got[0].Sender != "owner" {
		t.Fatalf("got %+v", got)
	}

	if err := s.agents.Drain(s.ctx); err != nil {
		t.Fatal(err)
	}

	if again := s.history(t, "petro", chat.ID); len(again) != 1 {
		t.Fatalf("greeted twice: %+v", again)
	}

	runs, err := s.agents.Runs(s.ctx, person("owner"), saved.Agents[0].ID)
	if err != nil || len(runs) != 1 || runs[0].Status != agent.StatusSent {
		t.Fatalf("got %+v, %v", runs, err)
	}
}

func TestAKeywordAgentDoesNotTalkToItself(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "owner", "petro")
	chat := s.group(t, "owner", "petro")
	saveAgent(t, s, "owner", agent.Agent{
		Name: "Цены", ChatID: chat.ID, Trigger: agent.TriggerKeyword, Keyword: "цена", Text: "Цена в описании, {name}",
	}, false)

	if _, err := s.messages.Send(s.ctx, person("petro"), chat.ID, "m1", "Какая ЦЕНА?"); err != nil {
		t.Fatal(err)
	}

	if _, err := s.messages.Send(s.ctx, person("petro"), chat.ID, "m2", "спасибо"); err != nil {
		t.Fatal(err)
	}

	for range 3 {
		if err := s.agents.Drain(s.ctx); err != nil {
			t.Fatal(err)
		}
	}

	got := s.history(t, "owner", chat.ID)
	if len(got) != 3 {
		t.Fatalf("the reply contains the keyword and must not trigger itself: %+v", got)
	}
}

func TestAnAgentStopsActingWhenItsOwnerLosesRights(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "owner", "admin", "petro")
	chat := s.group(t, "owner", "admin", "petro")

	if _, err := s.messages.AddMember(s.ctx, person("owner"), chat.ID, "admin", domain.RoleAdmin); err != nil {
		t.Fatal(err)
	}

	saved := saveAgent(t, s, "admin", agent.Agent{
		Name: "Цены", ChatID: chat.ID, Trigger: agent.TriggerKeyword, Keyword: "цена", Text: "Ответ",
	}, false)

	if _, err := s.messages.AddMember(s.ctx, person("owner"), chat.ID, "admin", domain.RoleMember); err != nil {
		t.Fatal(err)
	}

	if _, err := s.messages.Send(s.ctx, person("petro"), chat.ID, "m1", "цена?"); err != nil {
		t.Fatal(err)
	}

	if err := s.agents.Drain(s.ctx); err != nil {
		t.Fatal(err)
	}

	if got := s.history(t, "owner", chat.ID); len(got) != 1 {
		t.Fatalf("an agent without rights wrote: %+v", got)
	}

	runs, err := s.agents.Runs(s.ctx, person("admin"), saved.Agents[0].ID)
	if err != nil || len(runs) != 1 || runs[0].Status != agent.StatusFailed || runs[0].Detail != agent.DetailNoRights {
		t.Fatalf("got %+v, %v", runs, err)
	}
}

func TestAScheduledAgentRunsOnTimeAndPlansTheNext(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "owner")
	chat := s.group(t, "owner")
	saved := saveAgent(t, s, "owner", agent.Agent{
		Name: "Утро", ChatID: chat.ID, Trigger: agent.TriggerSchedule, Hour: 9, Weekdays: agent.AllWeekdays,
		Timezone: "UTC", Text: "Доброе утро",
	}, false)

	first := saved.Agents[0].NextRunAt
	if err := s.agents.Drain(s.ctx); err != nil {
		t.Fatal(err)
	}

	if got := s.history(t, "owner", chat.ID); len(got) != 0 {
		t.Fatalf("too early: %+v", got)
	}

	*s.clock = first.Add(time.Second)
	if err := s.agents.Drain(s.ctx); err != nil {
		t.Fatal(err)
	}

	if err := s.agents.Drain(s.ctx); err != nil {
		t.Fatal(err)
	}

	if got := s.history(t, "owner", chat.ID); len(got) != 1 || got[0].Text != "Доброе утро" {
		t.Fatalf("got %+v", got)
	}

	list, err := s.agents.List(s.ctx, person("owner"))
	if err != nil || !list[0].NextRunAt.After(first) {
		t.Fatalf("got %+v, %v", list, err)
	}
}

func TestAWebhookNeedsItsTokenAndHasALimit(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "owner")
	chat := s.group(t, "owner")
	saved := saveAgent(t, s, "owner", agent.Agent{Name: "CI", ChatID: chat.ID, Trigger: agent.TriggerWebhook}, true)

	if saved.WebhookToken == "" {
		t.Fatal("no token")
	}

	if err := s.agents.Webhook(s.ctx, "wrong", "x"); !errors.Is(err, agent.ErrNotFound) {
		t.Fatalf("got %v", err)
	}

	if err := s.agents.Webhook(s.ctx, saved.WebhookToken, "Сборка готова"); err != nil {
		t.Fatal(err)
	}

	if err := s.agents.Drain(s.ctx); err != nil {
		t.Fatal(err)
	}

	if got := s.history(t, "owner", chat.ID); len(got) != 1 || got[0].Text != "Сборка готова" {
		t.Fatalf("got %+v", got)
	}

	for range agent.MaxPendingHooks {
		if err := s.agents.Webhook(s.ctx, saved.WebhookToken, "x"); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.agents.Webhook(s.ctx, saved.WebhookToken, "x"); !errors.Is(err, agent.ErrTooManyHooks) {
		t.Fatalf("got %v", err)
	}

	var stored string

	err := s.pool.QueryRow(s.ctx, `SELECT webhook_hash FROM agents`).Scan(&stored)
	if err != nil || stored == saved.WebhookToken {
		t.Fatalf("the token is stored as is: %v", err)
	}
}

func TestAnAgentIsLimitedPerHour(t *testing.T) {
	s := newStage(t)
	saveProfiles(s.ctx, t, s.pool, "owner")
	chat := s.group(t, "owner")
	saved := saveAgent(t, s, "owner", agent.Agent{
		Name: "Эхо", ChatID: chat.ID, Trigger: agent.TriggerKeyword, Keyword: "эхо", Text: "Ответ",
	}, false)

	for index := range agent.MaxRunsPerHour + 5 {
		if _, err := s.messages.Send(s.ctx, person("owner"), chat.ID, "m"+time.Now().String(), "эхо"); err != nil {
			t.Fatal(err)
		}

		if index%50 == 0 {
			if err := s.agents.Drain(s.ctx); err != nil {
				t.Fatal(err)
			}
		}
	}

	for range 3 {
		if err := s.agents.Drain(s.ctx); err != nil {
			t.Fatal(err)
		}
	}

	var sent int

	err := s.pool.QueryRow(s.ctx, `SELECT count(*) FROM messages WHERE agent_id = $1`, saved.Agents[0].ID).Scan(&sent)
	if err != nil || sent != agent.MaxRunsPerHour {
		t.Fatalf("sent %d, %v", sent, err)
	}
}
