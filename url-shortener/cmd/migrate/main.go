package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"url-shortener/internal/config"
	"url-shortener/internal/db"
	"url-shortener/internal/logger"
)

func main() {
	databaseURL := config.MustDatabaseURL()
	log := logger.New(slog.LevelInfo)
	if err := run(databaseURL); err != nil {
		log.Error("migration failed", "error", err)
		os.Exit(1)
	}
	log.Info("database migrations complete")
}

func run(databaseURL string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	connectCtx, cancelConnect := context.WithTimeout(ctx, 10*time.Second)
	pool, err := db.Open(connectCtx, databaseURL)
	cancelConnect()
	if err != nil {
		return err
	}
	defer pool.Close()
	return db.Migrate(ctx, pool)
}
