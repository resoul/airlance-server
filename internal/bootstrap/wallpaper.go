package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"go.airlance.org/server/internal/domain/media"
	"go.airlance.org/server/internal/domain/settings"
	"go.airlance.org/server/internal/infrastructure/config"
	"go.airlance.org/server/internal/infrastructure/database"
	"go.airlance.org/server/internal/infrastructure/objectstore"
	"go.airlance.org/server/internal/repository/postgres"
)

const (
	wallpaperOwner = "system:wallpapers"
	idBytes        = 8
)

func WallpaperAdd(ctx context.Context, cfg config.Config, path, name string, sort int32) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return "", fmt.Errorf("database: %w", err)
	}
	defer pool.Close()

	files, err := objectstore.New(objectstore.Config(cfg.S3))
	if err != nil {
		return "", fmt.Errorf("file storage: %w", err)
	}

	if err := files.EnsureBucket(ctx); err != nil {
		return "", fmt.Errorf("file storage: %w", err)
	}

	id, mediaID := randomID(), randomID()+randomID()
	next := settings.Wallpaper{ID: id, Name: name, MediaID: mediaID, Sort: sort}

	if err := settings.CheckWallpaper(next); err != nil {
		return "", fmt.Errorf("wallpaper: %w", err)
	}

	now := time.Now()

	object, err := media.New(mediaID, wallpaperOwner, int64(len(data)), now)
	if err != nil {
		return "", fmt.Errorf("check file: %w", err)
	}

	if err := files.Put(ctx, mediaID, data); err != nil {
		return "", fmt.Errorf("store file: %w", err)
	}

	if err := postgres.NewMedia(pool).Create(ctx, object); err != nil {
		return "", fmt.Errorf("register file: %w", err)
	}

	if err := postgres.NewSettings(pool).AddWallpaper(ctx, next, now); err != nil {
		return "", fmt.Errorf("add wallpaper: %w", err)
	}

	return id, nil
}

func WallpaperList(ctx context.Context, cfg config.Config) ([]settings.Wallpaper, error) {
	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}
	defer pool.Close()

	list, err := postgres.NewSettings(pool).Wallpapers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list wallpapers: %w", err)
	}

	return list, nil
}

func WallpaperRemove(ctx context.Context, cfg config.Config, id string) error {
	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()

	files, err := objectstore.New(objectstore.Config(cfg.S3))
	if err != nil {
		return fmt.Errorf("file storage: %w", err)
	}

	mediaID, err := postgres.NewSettings(pool).RemoveWallpaper(ctx, id)
	if err != nil {
		return fmt.Errorf("remove wallpaper: %w", err)
	}

	if err := files.Remove(ctx, mediaID); err != nil {
		return fmt.Errorf("remove file: %w", err)
	}

	return nil
}

func randomID() string {
	raw := make([]byte, idBytes)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}

	return hex.EncodeToString(raw)
}
