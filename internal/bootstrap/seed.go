package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.airlance.org/server/internal/infrastructure/config"
	"go.airlance.org/server/internal/infrastructure/database"
	"go.airlance.org/server/internal/infrastructure/meili"
	"go.airlance.org/server/internal/infrastructure/objectstore"
	"go.airlance.org/server/internal/repository/postgres"
	"go.airlance.org/server/internal/usecase/contact"
	"go.airlance.org/server/internal/usecase/message"
	"go.airlance.org/server/internal/usecase/privacy"
	"go.airlance.org/server/internal/usecase/profile"
	"go.airlance.org/server/internal/usecase/seed"
)

const seedIDNode = 1

func SeedRun(ctx context.Context, cfg config.Config, logger *slog.Logger, viewer string) error {
	return withSeed(ctx, cfg, logger, func(service *seed.Service) error {
		return service.Run(ctx, strings.TrimPrefix(viewer, "@"))
	})
}

func SeedLive(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	return withSeed(ctx, cfg, logger, func(service *seed.Service) error {
		return service.Live(ctx)
	})
}

func SeedClean(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	return withSeed(ctx, cfg, logger, func(service *seed.Service) error {
		return service.Clean(ctx)
	})
}

func withSeed(ctx context.Context, cfg config.Config, logger *slog.Logger, work func(*seed.Service) error) error {
	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()

	files, err := objectstore.New(objectstore.Config(cfg.S3))
	if err != nil {
		return fmt.Errorf("file storage: %w", err)
	}

	if err := files.EnsureBucket(ctx); err != nil {
		return fmt.Errorf("file storage: %w", err)
	}

	wakes := postgres.NewWakes(pool, func(err error) {
		logger.ErrorContext(ctx, "wake-up failed", slog.Any("error", err))
	})
	clock := &seed.Clock{}
	guard := privacy.New(postgres.NewPrivacy(pool), wakes, time.Now)
	service := seed.New(seed.Deps{
		Messaging: message.New(
			postgres.NewMessages(pool),
			postgres.NewProfiles(pool),
			guard,
			wakes,
			message.NewIDs(seedIDNode, clock.Now),
			clock.Now,
		),
		Profiles: profile.New(postgres.NewProfiles(pool), postgres.NewMedia(pool), guard, wakes, time.Now),
		Contacts: contact.New(postgres.NewContacts(pool), postgres.NewPrivacy(pool), postgres.NewProfiles(pool), wakes, time.Now),
		Privacy:  guard,
		Media:    postgres.NewMedia(pool),
		Files:    files,
		Index:    meili.New(cfg.MeiliURL, cfg.MeiliKey),
		Store:    postgres.NewSeeds(pool),
		Notifier: wakes,
		Clock:    clock,
		Print: func(text string) {
			fmt.Println(text)
		},
	})

	return work(service)
}
