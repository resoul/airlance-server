package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "time/tzdata" // the image has no time zone files

	"github.com/spf13/cobra"

	"go.airlance.org/server/internal/bootstrap"
	"go.airlance.org/server/internal/infrastructure/config"
)

const healthTimeout = 5 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	err := newRootCommand(logger).ExecuteContext(ctx)

	stop()

	if err != nil {
		logger.Error("stopped", slog.Any("error", err))
		os.Exit(1)
	}
}

func newRootCommand(logger *slog.Logger) *cobra.Command {
	serve := func(cmd *cobra.Command, _ []string) error {
		cfg, err := config.Load(os.Getenv)
		if err != nil {
			return err
		}

		return bootstrap.Serve(cmd.Context(), cfg, logger)
	}

	root := &cobra.Command{
		Use:           "airlance-server",
		Short:         "The Airlance messenger server",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          serve,
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.AddCommand(
		&cobra.Command{Use: "serve", Short: "Run the server (the default)", Args: cobra.NoArgs, RunE: serve},
		newMigrateCommand(),
		newSeedCommand(logger),
		&cobra.Command{
			Use:   "healthcheck",
			Short: "Ask the running server whether it is ready",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return healthcheck(cmd.Context())
			},
		},
		&cobra.Command{
			Use:   "wireauth-public",
			Short: "Print the public WireAuth keys",
			Args:  cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error {
				return printWireAuthPublic()
			},
		},
	)

	return root
}

func needsSubcommand(cmd *cobra.Command, _ []string) error {
	return fmt.Errorf("%s needs a subcommand, see --help", cmd.Name())
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
