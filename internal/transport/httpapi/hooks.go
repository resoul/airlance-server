package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"go.airlance.org/server/internal/domain/agent"
)

const maxHookBody = 64 << 10

type HookReceiver interface {
	Webhook(ctx context.Context, token, text string) error
}

type Hooks struct {
	Receiver HookReceiver
	Logger   *slog.Logger
}

func (h Hooks) register(mux *http.ServeMux) {
	mux.HandleFunc("POST /hooks/{token}", h.receive)
}

func (h Hooks) receive(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxHookBody))
	if err != nil {
		w.WriteHeader(http.StatusRequestEntityTooLarge)

		return
	}

	var payload struct {
		Text string `json:"text"`
	}

	if json.Unmarshal(body, &payload) != nil {
		payload.Text = ""
	}

	err = h.Receiver.Webhook(r.Context(), r.PathValue("token"), payload.Text)

	switch {
	case err == nil:
		w.WriteHeader(http.StatusAccepted)
	case errors.Is(err, agent.ErrNotFound):
		w.WriteHeader(http.StatusNotFound)
	case errors.Is(err, agent.ErrTooManyHooks):
		w.WriteHeader(http.StatusTooManyRequests)
	default:
		h.Logger.ErrorContext(r.Context(), "webhook failed", slog.Any("error", err))
		w.WriteHeader(http.StatusInternalServerError)
	}
}
