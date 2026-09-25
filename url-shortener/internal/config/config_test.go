package config

import (
	"log/slog"
	"testing"
)

func TestMustLoad(t *testing.T) {
	setEnvironment(t)
	const databaseURL = "postgres://shortener:shortener@localhost:55439/shortener?sslmode=disable"
	for _, tc := range []struct {
		name      string
		url       string
		level     string
		wantLevel slog.Level
		wantPanic bool
	}{
		{name: "debug", url: databaseURL, level: "debug", wantLevel: slog.LevelDebug},
		{name: "info", url: databaseURL, level: "info", wantLevel: slog.LevelInfo},
		{name: "warn", url: databaseURL, level: "WARN", wantLevel: slog.LevelWarn},
		{name: "error", url: databaseURL, level: "error", wantLevel: slog.LevelError},
		{name: "whitespace", url: " " + databaseURL + " ", level: " debug ", wantLevel: slog.LevelDebug},
		{name: "missing URL", level: "info", wantPanic: true},
		{name: "blank URL", url: "  ", level: "info", wantPanic: true},
		{name: "missing level", url: databaseURL, wantPanic: true},
		{name: "invalid level", url: databaseURL, level: "verbose", wantPanic: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", tc.url)
			t.Setenv("LOG_LEVEL", tc.level)
			defer func() {
				if got := recover(); (got != nil) != tc.wantPanic {
					t.Errorf("panic = %v, want panic = %t", got, tc.wantPanic)
				}
			}()
			cfg := MustLoad()
			if cfg.DatabaseURL != databaseURL || cfg.LogLevel != tc.wantLevel {
				t.Errorf("unexpected config: %+v", cfg)
			}
		})
	}
}

func setEnvironment(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{
		"DATABASE_URL": "postgres://localhost/test", "LOG_LEVEL": "info",
		"ANALYTICS_HASH_KEY": "test-fingerprint-key-at-least-32-bytes", "ANALYTICS_QUEUE_SIZE": "10",
		"KAFKA_BROKERS": "localhost:19092", "KAFKA_TOPIC": "redirect-events-v1", "KAFKA_PARTITIONS": "4", "KAFKA_CONSUMER_GROUP": "test",
		"CLICKHOUSE_URL": "http://localhost:18123", "CLICKHOUSE_DB": "analytics", "CLICKHOUSE_USER": "analytics", "CLICKHOUSE_PASSWORD": "test",
	} {
		t.Setenv(key, value)
	}
}

func TestInvalidStreamAndStoreConfig(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"KAFKA_PARTITIONS", "0"}, {"KAFKA_PARTITIONS", "abc"}, {"KAFKA_BROKERS", "localhost:9092,"},
		{"ANALYTICS_QUEUE_SIZE", "-1"}, {"ANALYTICS_HASH_KEY", "short"},
		{"CLICKHOUSE_URL", "file:///tmp/test"}, {"CLICKHOUSE_DB", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setEnvironment(t)
			t.Setenv(tc.name, tc.value)
			defer func() {
				if recover() == nil {
					t.Error("invalid config should panic")
				}
			}()
			MustLoad()
		})
	}
}
