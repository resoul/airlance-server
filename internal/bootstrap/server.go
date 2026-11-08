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
	"go.airlance.org/server/internal/infrastructure/meili"
	"go.airlance.org/server/internal/infrastructure/objectstore"
	"go.airlance.org/server/internal/infrastructure/oidc"
	"go.airlance.org/server/internal/infrastructure/webfetch"
	"go.airlance.org/server/internal/repository/postgres"
	"go.airlance.org/server/internal/transport/httpapi"
	"go.airlance.org/server/internal/transport/wireauth"
	"go.airlance.org/server/internal/transport/ws"
	"go.airlance.org/server/internal/usecase/agent"
	"go.airlance.org/server/internal/usecase/contact"
	"go.airlance.org/server/internal/usecase/directory"
	"go.airlance.org/server/internal/usecase/folder"
	"go.airlance.org/server/internal/usecase/linkpreview"
	"go.airlance.org/server/internal/usecase/media"
	"go.airlance.org/server/internal/usecase/message"
	"go.airlance.org/server/internal/usecase/privacy"
	"go.airlance.org/server/internal/usecase/profile"
	"go.airlance.org/server/internal/usecase/scheduled"
	"go.airlance.org/server/internal/usecase/session"
	"go.airlance.org/server/internal/usecase/settings"
	"go.airlance.org/server/internal/usecase/story"
	"go.airlance.org/server/internal/usecase/update"
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

	hub := update.NewHub()
	updates := update.New(postgres.NewUpdates(pool), hub)
	index := meili.New(cfg.MeiliURL, cfg.MeiliKey)
	indexer := directory.NewIndexer(postgres.NewSearchQueue(pool), index, func(err error) {
		logger.ErrorContext(ctx, "search indexing failed", slog.Any("error", err))
	})
	wakes := postgres.NewWakes(pool, func(err error) {
		logger.ErrorContext(ctx, "update wake-ups failed", slog.Any("error", err))
	})
	guard := privacy.New(postgres.NewPrivacy(pool), hub, time.Now)
	profiles := profile.New(
		postgres.NewProfiles(pool),
		postgres.NewMedia(pool),
		guard,
		profile.Notifiers{hub, indexer},
		time.Now,
	)
	sessions := session.New(oidc.New(oidc.Config{Issuer: cfg.Issuer, Audience: cfg.Audience, JWKSURL: cfg.JWKSURL}))

	secure, err := wireauth.NewServer(cfg.WireAuthKeys)
	if err != nil {
		return fmt.Errorf("wireauth: %w", err)
	}

	files, err := objectstore.New(objectstore.Config(cfg.S3))
	if err != nil {
		return fmt.Errorf("file storage: %w", err)
	}

	if err := files.EnsureBucket(ctx); err != nil {
		return fmt.Errorf("file storage: %w", err)
	}

	attachments := media.New(postgres.NewMedia(pool), files, time.Now)
	stories := story.New(postgres.NewStories(pool), postgres.NewMedia(pool), files, hub, time.Now, func(err error) {
		logger.ErrorContext(ctx, "story cleanup failed", slog.Any("error", err))
	})
	messaging := message.New(
		postgres.NewMessages(pool),
		postgres.NewProfiles(pool),
		guard,
		hub,
		message.NewIDs(0, time.Now),
		time.Now,
	)
	scheduler := scheduled.New(postgres.NewScheduled(pool), messaging, hub, time.Now, func(err error) {
		logger.ErrorContext(ctx, "scheduled messages failed", slog.Any("error", err))
	})
	agents := agent.New(postgres.NewAgents(pool), messaging, time.Now, func(err error) {
		logger.ErrorContext(ctx, "agents failed", slog.Any("error", err))
	})
	finder := directory.New(index, guard, postgres.NewMessages(pool), time.Now)
	services := ws.Services{
		Profiles:  profiles,
		Updates:   updates,
		Media:     attachments,
		Stories:   stories,
		Privacy:   guard,
		Contacts:  contact.New(postgres.NewContacts(pool), postgres.NewPrivacy(pool), postgres.NewProfiles(pool), hub, time.Now),
		Directory: finder,
		Messaging: messaging,
		Scheduler: scheduler,
		Agents:    agents,
		Folders:   folder.New(postgres.NewFolders(pool), hub, time.Now),
		Settings:  settings.New(postgres.NewSettings(pool), hub, time.Now),
		Previews: linkpreview.New(
			postgres.NewLinkPreviews(pool), webfetch.New(), files, postgres.NewMedia(pool), time.Now,
		),
	}
	socket := ws.NewHandler(ctx, secure, sessions, services, ws.DefaultLimits(), logger)

	go wakes.Run(ctx, hub.Notify)
	go indexer.Run(ctx)
	go stories.Run(ctx)
	go scheduler.Run(ctx)
	go agents.Run(ctx)

	server := &http.Server{
		Addr: cfg.Addr,
		Handler: httpapi.NewRouter(pool.Ping, socket, httpapi.Links{
			Previews:   finder,
			AppleAppID: cfg.AppleAppID,
			Logger:     logger,
		}, httpapi.Hooks{Receiver: agents, Logger: logger}),
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
