package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL        string
	LogLevel           slog.Level
	FingerprintKey     string
	KafkaBrokers       []string
	KafkaTopic         string
	KafkaPartitions    int
	KafkaConsumerGroup string
	ProducerQueueSize  int
	ClickHouseURL      string
	ClickHouseDatabase string
	ClickHouseUser     string
	ClickHousePassword string
}

// MustLoad is the single place where application configuration reads the environment.
func MustLoad() Config {
	cfg := Config{
		DatabaseURL:        MustDatabaseURL(),
		FingerprintKey:     mustEnv("ANALYTICS_HASH_KEY"),
		KafkaBrokers:       strings.Split(mustEnv("KAFKA_BROKERS"), ","),
		KafkaTopic:         mustEnv("KAFKA_TOPIC"),
		KafkaPartitions:    mustPositive("KAFKA_PARTITIONS"),
		KafkaConsumerGroup: mustEnv("KAFKA_CONSUMER_GROUP"),
		ProducerQueueSize:  mustPositive("ANALYTICS_QUEUE_SIZE"),
		ClickHouseURL:      mustEnv("CLICKHOUSE_URL"),
		ClickHouseDatabase: mustEnv("CLICKHOUSE_DB"),
		ClickHouseUser:     mustEnv("CLICKHOUSE_USER"),
		ClickHousePassword: mustEnv("CLICKHOUSE_PASSWORD"),
	}
	if err := cfg.LogLevel.UnmarshalText([]byte(mustEnv("LOG_LEVEL"))); err != nil {
		panic(fmt.Errorf("invalid LOG_LEVEL: %w", err))
	}
	if len(cfg.FingerprintKey) < 32 {
		panic("ANALYTICS_HASH_KEY must be at least 32 bytes")
	}
	for i := range cfg.KafkaBrokers {
		cfg.KafkaBrokers[i] = strings.TrimSpace(cfg.KafkaBrokers[i])
		if cfg.KafkaBrokers[i] == "" {
			panic("KAFKA_BROKERS contains an empty broker")
		}
	}
	endpoint, err := url.Parse(cfg.ClickHouseURL)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		panic("CLICKHOUSE_URL must be an HTTP(S) URL")
	}
	return cfg
}

// MustDatabaseURL loads the database setting without requiring app or worker settings.
func MustDatabaseURL() string {
	return mustEnv("DATABASE_URL")
}

func mustEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		panic(name + " is required")
	}
	return value
}

func mustPositive(name string) int {
	value, err := strconv.Atoi(mustEnv(name))
	if err != nil || value <= 0 {
		panic(name + " must be a positive integer")
	}
	return value
}
