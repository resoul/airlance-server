package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"go.airlance.org/server/internal/infrastructure/config"
	"go.airlance.org/server/internal/infrastructure/database"
	"go.airlance.org/server/internal/infrastructure/oidc"
	"go.airlance.org/server/internal/repository/postgres"
	"go.airlance.org/server/internal/transport/httpapi"
	"go.airlance.org/server/internal/transport/ws"
	"go.airlance.org/server/internal/usecase/profile"
	"go.airlance.org/server/internal/usecase/session"
)

const (
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 10 * time.Second
)

func Serve(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()

	profiles := profile.New(postgres.NewProfiles(pool), time.Now)
	sessions := session.New(oidc.New(oidc.Config{Issuer: cfg.Issuer, Audience: cfg.Audience, JWKSURL: cfg.JWKSURL}))
	socket := ws.NewHandler(ctx, sessions, profiles, logger)
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpapi.NewRouter(pool.Ping, socket),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	failed := make(chan error, 1)
	go func() {
		logger.InfoContext(ctx, "listening", slog.String("addr", cfg.Addr))
		failed <- server.ListenAndServe()
	}()

	select {
	case err := <-failed:
		return fmt.Errorf("listen: %w", err)
	case <-ctx.Done():
	}

	stopping, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(stopping); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("shutdown: %w", err)
	}

	return nil
}
