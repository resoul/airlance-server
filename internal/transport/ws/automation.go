package ws

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.airlance.org/server/internal/domain/agent"
	"go.airlance.org/server/internal/domain/identity"
	"go.airlance.org/server/internal/domain/message"
	"go.airlance.org/server/internal/domain/scheduled"
	"go.airlance.org/server/internal/transport/codec"
	agents "go.airlance.org/server/internal/usecase/agent"
	schedule "go.airlance.org/server/internal/usecase/scheduled"
)

type Scheduler interface {
	Add(ctx context.Context, principal identity.Principal, draft schedule.Draft) ([]scheduled.Item, error)
	List(ctx context.Context, principal identity.Principal) ([]scheduled.Item, error)
	Edit(ctx context.Context, principal identity.Principal, id, text string, sendAt time.Time) ([]scheduled.Item, error)
	Cancel(ctx context.Context, principal identity.Principal, id string) ([]scheduled.Item, error)
	SendNow(ctx context.Context, principal identity.Principal, id string) ([]scheduled.Item, error)
}

type Agents interface {
	Save(ctx context.Context, principal identity.Principal, draft agent.Agent, rotate bool) (agents.Saved, error)
	List(ctx context.Context, principal identity.Principal) ([]agent.Agent, error)
	Delete(ctx context.Context, principal identity.Principal, id string) ([]agent.Agent, error)
	Runs(ctx context.Context, principal identity.Principal, id string) ([]agent.Run, error)
}

func (h *Handler) schedulerRequest(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	var (
		items []scheduled.Item
		err   error
	)

	switch request.Kind {
	case codec.KindScheduledAdd:
		add, decodeErr := codec.DecodeScheduledAdd(request.Body)
		if decodeErr != nil {
			return failure(request, codeInvalid, "invalid body")
		}

		items, err = h.scheduler.Add(ctx, principal, schedule.Draft{
			ClientID: add.ClientID,
			ChatID:   add.ChatID,
			Text:     add.Text,
			SendAt:   time.UnixMilli(add.SendAtMs),
			Repeat:   scheduled.Repeat(add.Repeat),
			Timezone: add.Timezone,
		})
	case codec.KindScheduledEdit:
		edit, decodeErr := codec.DecodeScheduledEdit(request.Body)
		if decodeErr != nil {
			return failure(request, codeInvalid, "invalid body")
		}

		items, err = h.scheduler.Edit(ctx, principal, edit.ID, edit.Text, time.UnixMilli(edit.SendAtMs))
	case codec.KindScheduledCancel:
		id, decodeErr := codec.DecodeScheduledCancel(request.Body)
		if decodeErr != nil {
			return failure(request, codeInvalid, "invalid body")
		}

		items, err = h.scheduler.Cancel(ctx, principal, id)
	case codec.KindScheduledSendNow:
		id, decodeErr := codec.DecodeScheduledSendNow(request.Body)
		if decodeErr != nil {
			return failure(request, codeInvalid, "invalid body")
		}

		items, err = h.scheduler.SendNow(ctx, principal, id)
	default:
		items, err = h.scheduler.List(ctx, principal)
	}

	if err != nil {
		return h.automationFailure(ctx, request, err)
	}

	rows := make([]codec.ScheduledItem, len(items))
	for index, next := range items {
		rows[index] = codec.ScheduledItem{
			ID:       next.ID,
			ChatID:   next.ChatID,
			ClientID: next.ClientID,
			Text:     next.Text,
			SendAtMs: next.SendAt.UnixMilli(),
			Repeat:   int32(next.Repeat),
			Failure:  next.Failure,
		}
	}

	return reply(request, codec.KindScheduledData, codec.EncodeScheduledData(rows))
}

func (h *Handler) agentRequest(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	switch request.Kind {
	case codec.KindAgentSave:
		return h.agentSave(ctx, principal, request)
	case codec.KindAgentDelete:
		id, err := codec.DecodeAgentDelete(request.Body)
		if err != nil {
			return failure(request, codeInvalid, "invalid body")
		}

		found, err := h.agents.Delete(ctx, principal, id)

		return h.agentsReply(ctx, request, found, "", err)
	case codec.KindAgentRunsGet:
		return h.agentRuns(ctx, principal, request)
	default:
		found, err := h.agents.List(ctx, principal)

		return h.agentsReply(ctx, request, found, "", err)
	}
}

func (h *Handler) agentSave(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	save, err := codec.DecodeAgentSave(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	data := save.Agent
	saved, err := h.agents.Save(ctx, principal, agent.Agent{
		ID:       data.ID,
		ClientID: data.ClientID,
		Name:     data.Name,
		ChatID:   data.ChatID,
		Trigger:  agent.Trigger(data.Trigger),
		Keyword:  data.Keyword,
		Hour:     int(data.Hour),
		Minute:   int(data.Minute),
		Weekdays: int(data.Weekdays),
		Timezone: data.Timezone,
		Text:     data.Text,
		Enabled:  data.Enabled,
	}, save.Rotate)

	return h.agentsReply(ctx, request, saved.Agents, saved.WebhookToken, err)
}

func (h *Handler) agentsReply(
	ctx context.Context,
	request codec.Frame,
	found []agent.Agent,
	token string,
	err error,
) codec.Frame {
	if err != nil {
		return h.automationFailure(ctx, request, err)
	}

	rows := make([]codec.AgentData, len(found))
	for index, next := range found {
		rows[index] = codec.AgentData{
			ID:        next.ID,
			ClientID:  next.ClientID,
			Name:      next.Name,
			ChatID:    next.ChatID,
			ChatTitle: next.ChatTitle,
			Trigger:   int32(next.Trigger),
			Keyword:   next.Keyword,
			Hour:      int32(next.Hour),
			Minute:    int32(next.Minute),
			Weekdays:  int32(next.Weekdays),
			Timezone:  next.Timezone,
			Text:      next.Text,
			Enabled:   next.Enabled,
			NextRunMs: next.NextRunAt.UnixMilli(),
		}
	}

	return reply(request, codec.KindAgentsData, codec.EncodeAgentsData(rows, token))
}

func (h *Handler) agentRuns(ctx context.Context, principal identity.Principal, request codec.Frame) codec.Frame {
	id, err := codec.DecodeAgentRunsGet(request.Body)
	if err != nil {
		return failure(request, codeInvalid, "invalid body")
	}

	runs, err := h.agents.Runs(ctx, principal, id)
	if err != nil {
		return h.automationFailure(ctx, request, err)
	}

	rows := make([]codec.AgentRunData, len(runs))
	for index, next := range runs {
		rows[index] = codec.AgentRunData{Status: int32(next.Status), Detail: next.Detail, RanAtMs: next.RanAt.UnixMilli()}
	}

	return reply(request, codec.KindAgentRunsData, codec.EncodeAgentRunsData(rows))
}

func (h *Handler) automationFailure(ctx context.Context, request codec.Frame, err error) codec.Frame {
	switch {
	case errors.Is(err, scheduled.ErrInvalidTime):
		return failure(request, codeUnprocessable, "invalid_time")
	case errors.Is(err, scheduled.ErrInvalidRepeat):
		return failure(request, codeUnprocessable, "invalid_repeat")
	case errors.Is(err, scheduled.ErrInvalidZone), errors.Is(err, agent.ErrInvalidZone):
		return failure(request, codeUnprocessable, "invalid_timezone")
	case errors.Is(err, scheduled.ErrTooMany), errors.Is(err, agent.ErrTooMany):
		return failure(request, codeConflict, "too_many")
	case errors.Is(err, scheduled.ErrNotFound):
		return failure(request, codeNotFound, "scheduled_not_found")
	case errors.Is(err, agent.ErrNotFound):
		return failure(request, codeNotFound, "agent_not_found")
	case errors.Is(err, agent.ErrInvalidName):
		return failure(request, codeUnprocessable, "invalid_name")
	case errors.Is(err, agent.ErrInvalidTrigger):
		return failure(request, codeUnprocessable, "invalid_trigger")
	case errors.Is(err, agent.ErrInvalidKeyword):
		return failure(request, codeUnprocessable, "invalid_keyword")
	case errors.Is(err, agent.ErrInvalidTime):
		return failure(request, codeUnprocessable, "invalid_time")
	case errors.Is(err, message.ErrInvalidText), errors.Is(err, message.ErrInvalidClientID),
		errors.Is(err, message.ErrChatNotFound), errors.Is(err, message.ErrNotAllowed),
		errors.Is(err, message.ErrBlocked), errors.Is(err, identity.ErrForbidden):
		return h.chatFailure(ctx, request, err)
	default:
		h.logger.ErrorContext(ctx, "automation request failed", slog.Any("error", err))

		return failure(request, codeInternal, "internal")
	}
}
