package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"url-shortener/internal/config"
	"url-shortener/internal/handlers"
	"url-shortener/internal/httpserver"
	"url-shortener/internal/logger"
	"url-shortener/internal/metrics"
	"url-shortener/internal/store/chstore"
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

	startupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	kafkastream.MustEnsureTopic(startupCtx, cfg.KafkaBrokers, cfg.KafkaTopic, cfg.KafkaPartitions)

	eventStore := &chstore.Store{
		URL: cfg.ClickHouseURL, Database: cfg.ClickHouseDatabase,
		Username: cfg.ClickHouseUser, Password: cfg.ClickHousePassword,
		Client: &http.Client{Timeout: 10 * time.Second},
	}
	eventStore.MustOpen(startupCtx)
	cancel()

	reader := kafkastream.NewKafkaReader(cfg.KafkaBrokers, cfg.KafkaTopic, cfg.KafkaConsumerGroup)
	defer reader.Close()

	var consumer stream.Consumer = &kafkastream.Consumer{Reader: reader, Store: eventStore, Logger: log, Metrics: appMetrics.Analytics}

	instanceID := httpserver.MustHostname()
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.Handle("GET /healthz", handlers.NewGetHealthzHandler(instanceID))

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           appMetrics.HTTPMiddleware(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	listener := httpserver.MustListen(srv.Addr)
	defer listener.Close()
	go httpserver.MustServe(srv, listener)

	log.Info("analytics worker ready", "instanceID", instanceID, "topic", cfg.KafkaTopic, "group", cfg.KafkaConsumerGroup, "partitions", cfg.KafkaPartitions)

	if err := consumer.Run(ctx); err != nil {
		panic(err)
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()

	httpserver.MustShutdown(shutdownCtx, srv)

	log.Info("bye")
}
