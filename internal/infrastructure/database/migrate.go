package database

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // registers the pgx5 scheme
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"go.airlance.org/server/migrations"
)

const (
	migrationsTable  = "airlance_server_migrations"
	migrationsScheme = "pgx5"
)

type MigrationState struct {
	Version uint
	IsDirty bool
	Applied bool
}

func Migrate(ctx context.Context, databaseURL string) error {
	return withMigrator(ctx, databaseURL, func(migrator *migrate.Migrate) error {
		if err := ignoreNoChange(migrator.Up()); err != nil {
			return fmt.Errorf("apply migrations: %w", err)
		}

		return nil
	})
}

func MigrateDown(ctx context.Context, databaseURL string, steps int) error {
	if steps < 1 {
		return errors.New("steps must be greater than zero")
	}

	return withMigrator(ctx, databaseURL, func(migrator *migrate.Migrate) error {
		err := migrator.Steps(-steps)
		if errors.Is(err, migrate.ErrNoChange) || errors.Is(err, migrate.ErrNilVersion) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("roll back migrations: %w", err)
		}

		return nil
	})
}

func MigrateReset(ctx context.Context, databaseURL string) error {
	return withMigrator(ctx, databaseURL, func(migrator *migrate.Migrate) error {
		if err := ignoreNoChange(migrator.Down()); err != nil {
			return fmt.Errorf("reset migrations: %w", err)
		}

		return nil
	})
}

func MigrationVersion(ctx context.Context, databaseURL string) (MigrationState, error) {
	var state MigrationState

	err := withMigrator(ctx, databaseURL, func(migrator *migrate.Migrate) error {
		version, isDirty, err := migrator.Version()
		if errors.Is(err, migrate.ErrNilVersion) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("read migration version: %w", err)
		}

		state = MigrationState{Version: version, IsDirty: isDirty, Applied: true}

		return nil
	})

	return state, err
}

func ignoreNoChange(err error) error {
	if errors.Is(err, migrate.ErrNoChange) {
		return nil
	}

	return err
}

func withMigrator(ctx context.Context, databaseURL string, work func(*migrate.Migrate) error) (err error) {
	source, err := iofs.New(migrations.Files, ".")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	address, err := migrationURL(databaseURL)
	if err != nil {
		return err
	}

	migrator, err := migrate.NewWithSourceInstance("iofs", source, address)
	if err != nil {
		return fmt.Errorf("prepare migrations: %w", err)
	}

	defer func() {
		sourceErr, databaseErr := migrator.Close()
		if err == nil {
			err = errors.Join(sourceErr, databaseErr)
		}
	}()

	stop := context.AfterFunc(ctx, func() {
		migrator.GracefulStop <- true
	})
	defer stop()

	return work(migrator)
}

func migrationURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		return "", errors.New("the database address must be a postgres:// or postgresql:// URL")
	}

	parsed.Scheme = migrationsScheme
	query := parsed.Query()
	query.Set("x-migrations-table", migrationsTable)
	parsed.RawQuery = query.Encode()

	return parsed.String(), nil
}
