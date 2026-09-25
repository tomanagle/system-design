package logger

import (
	"os"

	"log/slog"
)

type Logger struct {
	logger *slog.Logger
}

func NewLogger() *Logger {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	return &Logger{logger: logger}
}
