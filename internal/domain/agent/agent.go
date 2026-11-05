package agent

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"go.airlance.org/server/internal/domain/message"
)

type Trigger int16

const (
	TriggerSchedule Trigger = 0
	TriggerJoined   Trigger = 1
	TriggerKeyword  Trigger = 2
	TriggerWebhook  Trigger = 3
)

type Status int16

const (
	StatusSent    Status = 0
	StatusSkipped Status = 1
	StatusFailed  Status = 2
)

const (
	MaxPerOwner     = 20
	MaxRunsPerHour  = 60
	MaxPendingHooks = 100
	KeptRuns        = 50
	MaxRunsPage     = 50
	MaxNameLength   = 64
	MaxKeyword      = 64
	AllWeekdays     = 127
	NamePlaceholder = "{name}"
	TextPlaceholder = "{text}"
)

const (
	DetailNoRights    = "no_rights"
	DetailRateLimited = "rate_limited"
	DetailEmpty       = "empty"
	DetailKindJoined  = "joined"
	DetailKindKeyword = "keyword"
	DetailKindHook    = "webhook"
	DetailKindTime    = "schedule"
)

var (
	ErrInvalidName    = errors.New("invalid agent name")
	ErrInvalidTrigger = errors.New("invalid agent trigger")
	ErrInvalidKeyword = errors.New("invalid agent keyword")
	ErrInvalidTime    = errors.New("invalid agent time")
	ErrInvalidZone    = errors.New("invalid agent time zone")
	ErrTooMany        = errors.New("too many agents")
	ErrNotFound       = errors.New("agent not found")
	ErrTooManyHooks   = errors.New("too many webhook calls")
)

type Agent struct {
	ID        string
	Owner     string
	ClientID  string
	Name      string
	ChatID    string
	ChatTitle string
	Trigger   Trigger
	Keyword   string
	Hour      int
	Minute    int
	Weekdays  int
	Timezone  string
	Text      string
	Enabled   bool
	NextRunAt time.Time
}

type Run struct {
	Status Status
	Detail string
	RanAt  time.Time
}

type Event struct {
	ID      int64
	Trigger Trigger
	ChatID  string
	AgentID string
	Subject string
	Text    string
}

func (a Agent) Checked() (Agent, error) {
	a.Name = strings.TrimSpace(a.Name)
	if a.Name == "" || !utf8.ValidString(a.Name) || utf8.RuneCountInString(a.Name) > MaxNameLength {
		return Agent{}, ErrInvalidName
	}

	if a.ClientID == "" || len(a.ClientID) > message.MaxClientIDLength || !utf8.ValidString(a.ClientID) {
		return Agent{}, message.ErrInvalidClientID
	}

	a.Text = strings.TrimSpace(a.Text)
	if !utf8.ValidString(a.Text) || utf8.RuneCountInString(a.Text) > message.MaxTextLength {
		return Agent{}, message.ErrInvalidText
	}

	if a.Text == "" && a.Trigger != TriggerWebhook {
		return Agent{}, message.ErrInvalidText
	}

	return a.checkTrigger()
}

func (a Agent) checkTrigger() (Agent, error) {
	switch a.Trigger {
	case TriggerSchedule:
		return a.checkSchedule()
	case TriggerKeyword:
		a.Keyword = strings.TrimSpace(a.Keyword)
		if a.Keyword == "" || !utf8.ValidString(a.Keyword) || utf8.RuneCountInString(a.Keyword) > MaxKeyword {
			return Agent{}, ErrInvalidKeyword
		}

		return a, nil
	case TriggerJoined, TriggerWebhook:
		return a, nil
	default:
		return Agent{}, ErrInvalidTrigger
	}
}

func (a Agent) checkSchedule() (Agent, error) {
	if a.Hour < 0 || a.Hour > 23 || a.Minute < 0 || a.Minute > 59 || a.Weekdays < 1 || a.Weekdays > AllWeekdays {
		return Agent{}, ErrInvalidTime
	}

	if a.Timezone == "" {
		a.Timezone = time.UTC.String()
	}

	if _, err := time.LoadLocation(a.Timezone); err != nil {
		return Agent{}, fmt.Errorf("%w: %s", ErrInvalidZone, a.Timezone)
	}

	return a, nil
}

func (a Agent) NextRun(now time.Time) time.Time {
	zone, err := time.LoadLocation(a.Timezone)
	if err != nil {
		zone = time.UTC
	}

	local := now.In(zone)
	day := time.Date(local.Year(), local.Month(), local.Day(), a.Hour, a.Minute, 0, 0, zone)

	for range 8 {
		if day.After(now) && a.Weekdays&(1<<int(day.Weekday())) != 0 {
			return day
		}

		day = time.Date(day.Year(), day.Month(), day.Day()+1, a.Hour, a.Minute, 0, 0, zone)
	}

	return day
}

func (a Agent) Matches(event Event) bool {
	if a.Trigger != event.Trigger || a.ChatID != event.ChatID || !a.Enabled {
		return false
	}

	switch a.Trigger {
	case TriggerKeyword:
		return strings.Contains(strings.ToLower(event.Text), strings.ToLower(a.Keyword))
	case TriggerWebhook:
		return a.ID == event.AgentID
	case TriggerJoined:
		return true
	case TriggerSchedule:
		return false
	}

	return false
}

func (a Agent) Render(personName, payload string) string {
	if a.Text == "" {
		return limited(strings.TrimSpace(payload))
	}

	text := strings.NewReplacer(NamePlaceholder, personName, TextPlaceholder, payload).Replace(a.Text)

	return limited(strings.TrimSpace(text))
}

func (t Trigger) Detail() string {
	switch t {
	case TriggerJoined:
		return DetailKindJoined
	case TriggerKeyword:
		return DetailKindKeyword
	case TriggerWebhook:
		return DetailKindHook
	case TriggerSchedule:
		return DetailKindTime
	}

	return ""
}

func limited(text string) string {
	if utf8.RuneCountInString(text) <= message.MaxTextLength {
		return text
	}

	return string([]rune(text)[:message.MaxTextLength])
}
