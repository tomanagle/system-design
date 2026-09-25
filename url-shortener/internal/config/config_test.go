package config

import (
	"log/slog"
	"testing"
)

func TestMustLoad(t *testing.T) {
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
