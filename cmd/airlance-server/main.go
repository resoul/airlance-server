package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.airlance.org/server/internal/bootstrap"
	"go.airlance.org/server/internal/infrastructure/config"
	"go.airlance.org/server/internal/infrastructure/database"
)

const healthTimeout = 5 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	err := run(ctx, os.Args[1:], logger)

	stop()

	if err != nil {
		logger.Error("stopped", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, logger *slog.Logger) error {
	command := "serve"
	if len(args) > 0 {
		command = args[0]
	}

	switch command {
	case "healthcheck":
		return healthcheck(ctx)
	case "wireauth-public":
		return printWireAuthPublic()
	}

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	switch command {
	case "serve":
		return bootstrap.Serve(ctx, cfg, logger)
	case "migrate":
		return database.Migrate(ctx, cfg.DatabaseURL)
	default:
		return fmt.Errorf("unknown command %q: serve, migrate, healthcheck or wireauth-public", command)
	}
}

func printWireAuthPublic() error {
	keys, err := config.LoadWireAuthKeys(os.Getenv("AIRLANCE_SERVER_WIREAUTH_KEYS"))
	if err != nil {
		return err
	}

	for id, key := range keys {
		public, ok := key.Public().(ed25519.PublicKey)
		if !ok {
			return errors.New("not an Ed25519 key")
		}

		fmt.Printf("%d %s\n", id, base64.StdEncoding.EncodeToString(public))
	}

	return nil
}

func healthcheck(ctx context.Context) error {
	addr := os.Getenv("AIRLANCE_SERVER_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8090"
	}

	if _, port, found := strings.Cut(addr, ":"); found {
		addr = "127.0.0.1:" + port
	}

	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/health/ready", http.NoBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("health request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return errors.New("not ready")
	}

	return nil
}
