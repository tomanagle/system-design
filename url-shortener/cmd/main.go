package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"url-shortener/internal/config"
	"url-shortener/internal/db"
	"url-shortener/internal/handlers"
	"url-shortener/internal/httpserver"
	"url-shortener/internal/logger"
	"url-shortener/internal/metrics"
	"url-shortener/internal/shortcode"
	"url-shortener/internal/store/pgstore"
	"url-shortener/internal/stream"
	"url-shortener/internal/stream/kafkastream"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	cfg := config.MustLoad()
	registry := prometheus.NewRegistry()
	registry.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	appMetrics := metrics.MustNew(registry)
	log := logger.New(cfg.LogLevel)

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

	log.Info("connected to postgres")

	r := http.NewServeMux()
	instanceID := httpserver.MustHostname()

	shortURLStore := pgstore.NewShortUrlStore(pgstore.ShortUrlStoreParams{
		DB:            pool,
		CodeGenerator: shortcode.Generate,
		Metrics:       appMetrics.Store,
		Logger:        log,
	})
	producer := kafkastream.NewProducer(kafkastream.ProducerParams{
		Writer:    kafkastream.NewKafkaWriter(cfg.KafkaBrokers, cfg.KafkaTopic),
		Logger:    log,
		QueueSize: cfg.ProducerQueueSize,
		Metrics:   appMetrics.Producer,
	})
	metrics.MustRegisterProducerQueue(registry, producer.QueueDepth, producer.QueueCapacity())
	defer func() {
		drainCtx, cancelDrain := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelDrain()
		producer.Close(drainCtx)
	}()

	r.Handle("GET /healthz", handlers.NewGetHealthzHandler(instanceID))

	r.Handle("GET /metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	r.Handle("POST /short-url", handlers.NewPostShortURLHandler(handlers.PostShortURLHandlerParams{
		ShortURLStore: shortURLStore,
	}))

	eventFactory := stream.NewEventGenerator([]byte(cfg.FingerprintKey))

	r.Handle("GET /", handlers.NewGetRedirectHandler(handlers.GetRedirectParams{
		ShortURLStore: shortURLStore,
		Producer:      producer,
		Redirects:     appMetrics.ShortURLRedirects,
		Logger:        log,
		EventFactory:  eventFactory,
	}))

	srv := &http.Server{
		Addr:    ":8080",
		Handler: appMetrics.HTTPMiddleware(r),
	}

	listener := httpserver.MustListen(srv.Addr)
	defer listener.Close()
	go httpserver.MustServe(srv, listener)

	log.Info("ready to work", "instanceID", instanceID)

	<-ctx.Done()

	log.Info("shutting down server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	httpserver.MustShutdown(shutdownCtx, srv)

	log.Info("bye")
}
