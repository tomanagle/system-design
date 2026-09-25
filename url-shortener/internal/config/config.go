package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

type Config struct {
	DatabaseURL string
	LogLevel    slog.Level
}

// MustLoad reads the required environment variables and panics on invalid config.
func MustLoad() Config {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		panic("DATABASE_URL is required")
	}

	level := strings.TrimSpace(os.Getenv("LOG_LEVEL"))
	if level == "" {
		panic("LOG_LEVEL is required")
	}
	var logLevel slog.Level
	if err := logLevel.UnmarshalText([]byte(level)); err != nil {
		panic(fmt.Errorf("invalid LOG_LEVEL: %w", err))
	}

	return Config{
		DatabaseURL: databaseURL,
		LogLevel:    logLevel,
	}
}
