package main

import (
	"errors"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"go.airlance.org/server/internal/bootstrap"
	"go.airlance.org/server/internal/infrastructure/config"
)

func newSeedCommand(logger *slog.Logger) *cobra.Command {
	var viewer string

	withConfig := func(work func(*cobra.Command, config.Config) error) func(*cobra.Command, []string) error {
		return func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(os.Getenv)
			if err != nil {
				return err
			}

			return work(cmd, cfg)
		}
	}

	root := &cobra.Command{
		Use:   "seed",
		Short: "Write test people, groups and channels",
		Args:  cobra.NoArgs,
		RunE:  needsSubcommand,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			if os.Getenv("AIRLANCE_SEED") != "1" {
				return errors.New("seed writes test people and chats: set AIRLANCE_SEED=1 to allow it")
			}

			return nil
		},
	}

	run := &cobra.Command{
		Use:   "run",
		Short: "Create the test data, a repeat changes nothing",
		Args:  cobra.NoArgs,
		RunE: withConfig(func(cmd *cobra.Command, cfg config.Config) error {
			return bootstrap.SeedRun(cmd.Context(), cfg, logger, viewer)
		}),
	}
	run.Flags().StringVar(&viewer, "to", "", "the username of an account to add to the chats")

	root.AddCommand(
		run,
		&cobra.Command{
			Use:   "live",
			Short: "Write new messages for 10 minutes",
			Args:  cobra.NoArgs,
			RunE: withConfig(func(cmd *cobra.Command, cfg config.Config) error {
				return bootstrap.SeedLive(cmd.Context(), cfg, logger)
			}),
		},
		&cobra.Command{
			Use:   "clean",
			Short: "Remove everything seed created",
			Args:  cobra.NoArgs,
			RunE: withConfig(func(cmd *cobra.Command, cfg config.Config) error {
				return bootstrap.SeedClean(cmd.Context(), cfg, logger)
			}),
		},
	)

	return root
}
