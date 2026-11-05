package httpapi

import (
	"context"
	"net/http"
)

type Readiness func(ctx context.Context) error

func NewRouter(ready Readiness, socket http.Handler, links Links, hooks Hooks) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/alive", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if ready(r.Context()) != nil {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("GET /v1/ws", socket)
	links.register(mux)
	hooks.register(mux)

	return mux
}
