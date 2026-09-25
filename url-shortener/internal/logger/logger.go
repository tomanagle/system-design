package logger

import (
	"log/slog"
	"os"
)

// New builds the JSON logger every binary writes to stdout.
func New(level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
}
