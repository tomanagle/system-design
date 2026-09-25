package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"url-shortener/internal/config"
	"url-shortener/internal/db"
	"url-shortener/internal/handlers"
	"url-shortener/internal/metrics"
	"url-shortener/internal/shortcode"
	"url-shortener/internal/store/dbstore"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	cfg := config.MustLoad()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	connectCtx, cancelConnect := context.WithTimeout(ctx, 10*time.Second)
	defer cancelConnect()

	database := db.NewDB(db.NewDBParams{
		DatabaseURL: cfg.DatabaseURL,
	})

	pool := database.MustOpen(connectCtx)
	defer pool.Close()

	cancelConnect()

	logger.Info("connected to postgres")

	r := http.NewServeMux()
	instanceID := MustHostname()

	shortURLStore := dbstore.NewShortUrlStore(dbstore.ShortUrlStoreParams{
		DB:            pool,
		CodeGenerator: shortcode.Generate,
		Logger:        logger,
	})

	r.Handle("GET /healthz", handlers.NewGetHealthzHandler(instanceID))

	r.Handle("GET /metrics", promhttp.Handler())

	r.Handle("POST /short-url", handlers.NewPostShortURLHandler(handlers.PostShortURLHandlerParams{
		ShortURLStore: shortURLStore,
	}))

	r.Handle("GET /", handlers.NewGetRedirectHandler(handlers.GetRedirectParams{
		ShortURLStore: shortURLStore,
	}))

	srv := &http.Server{
		Addr:    ":8080",
		Handler: metrics.HTTPMiddleware(r),
	}

	listener := MustListen(srv.Addr)
	defer listener.Close()
	go MustServe(srv, listener)

	logger.Info("ready to work", "instanceID", instanceID)

	<-ctx.Done()

	logger.Info("shutting down server")

	// Create a context with a timeout for shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	MustShutdown(shutdownCtx, srv)

	logger.Info("bye")
}
