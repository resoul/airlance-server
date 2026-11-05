package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/spf13/cobra"

	"go.airlance.org/server/internal/infrastructure/config"
	"go.airlance.org/server/internal/infrastructure/database"
)

const (
	migrationsDirectory = "migrations"
	migrationFileMode   = 0o600
)

var (
	migrationName = regexp.MustCompile(`^[a-z0-9][a-z0-9_]*$`)
	migrationFile = regexp.MustCompile(`^(\d+)_.*\.(up|down)\.sql$`)
)

func newMigrateCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "migrate",
		Short: "Manage the database schema",
		Args:  cobra.NoArgs,
		RunE:  needsSubcommand,
	}
	root.AddCommand(
		&cobra.Command{
			Use:   "up",
			Short: "Apply all pending migrations",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				address, err := databaseURL()
				if err != nil {
					return err
				}

				return database.Migrate(cmd.Context(), address)
			},
		},
		newMigrateDownCommand(),
		newMigrateResetCommand(),
		&cobra.Command{
			Use:   "version",
			Short: "Show the current schema version",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				address, err := databaseURL()
				if err != nil {
					return err
				}

				state, err := database.MigrationVersion(cmd.Context(), address)
				if err != nil {
					return err
				}

				printVersion(state)

				return nil
			},
		},
		&cobra.Command{
			Use:   "create NAME",
			Short: "Create a pair of empty migration files, run from the project folder",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return createMigration(migrationsDirectory, args[0])
			},
		},
	)

	return root
}

func newMigrateDownCommand() *cobra.Command {
	var steps int

	down := &cobra.Command{
		Use:   "down",
		Short: "Roll back migrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			address, err := databaseURL()
			if err != nil {
				return err
			}

			return database.MigrateDown(cmd.Context(), address, steps)
		},
	}
	down.Flags().IntVar(&steps, "steps", 1, "how many migrations to roll back")

	return down
}

func newMigrateResetCommand() *cobra.Command {
	var force bool

	reset := &cobra.Command{
		Use:   "reset",
		Short: "Roll back all migrations, this deletes all data",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !force {
				return errors.New("reset deletes all data: pass --force to confirm")
			}

			address, err := databaseURL()
			if err != nil {
				return err
			}

			return database.MigrateReset(cmd.Context(), address)
		},
	}
	reset.Flags().BoolVar(&force, "force", false, "confirm deleting all data")

	return reset
}

func databaseURL() (string, error) {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return "", err
	}

	return cfg.DatabaseURL, nil
}

func printVersion(state database.MigrationState) {
	if !state.Applied {
		fmt.Println("No migrations applied yet")

		return
	}

	fmt.Printf("Version %d, dirty: %t\n", state.Version, state.IsDirty)
}

func createMigration(directory, name string) error {
	if !migrationName.MatchString(name) {
		return errors.New("a migration name holds only a-z, 0-9 and _")
	}

	files, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	latest := 0

	for _, file := range files {
		found := migrationFile.FindStringSubmatch(file.Name())
		if found == nil {
			continue
		}

		if version, err := strconv.Atoi(found[1]); err == nil && version > latest {
			latest = version
		}
	}

	stem := filepath.Join(directory, fmt.Sprintf("%06d_%s", latest+1, name))
	up, down := stem+".up.sql", stem+".down.sql"

	if err := createEmpty(up); err != nil {
		return err
	}

	if err := createEmpty(down); err != nil {
		return errors.Join(err, os.Remove(up))
	}

	fmt.Println(up)
	fmt.Println(down)

	return nil
}

func createEmpty(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, migrationFileMode)
	if err != nil {
		return fmt.Errorf("create migration: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("close migration: %w", err)
	}

	return nil
}
