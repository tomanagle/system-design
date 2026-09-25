package kafkastream

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const retryDelay = time.Second

// retry runs op until it succeeds or ctx ends, reporting false if it never did.
// Errors seen while ctx is already ending are shutdown noise, not failures.
func retry(ctx context.Context, logger *slog.Logger, message string, failures prometheus.Counter, op func() error) bool {
	for ctx.Err() == nil {
		err := op()
		if err == nil {
			return true
		}
		if ctx.Err() != nil {
			break
		}
		failures.Inc()
		logger.Warn(message, "error", err)
		if !wait(ctx, retryDelay) {
			break
		}
	}
	return false
}

// wait sleeps for delay, reporting false if ctx ended first.
func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
