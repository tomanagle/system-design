package metrics

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name: "http_request_duration_seconds",
	Help: "Duration of completed HTTP requests, including writing the response.",
}, []string{"route", "method", "code"})

var dbDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name: "db_request_duration_seconds",
	Help: "Duration of database store operations, including code generation, pool wait and result scanning.",
}, []string{"operation", "outcome"})

var ShortURLsCreated = promauto.NewCounter(prometheus.CounterOpts{
	Name: "short_urls_created_total",
	Help: "Total number of successfully created short URLs.",
})

var ShortURLRedirects = promauto.NewCounter(prometheus.CounterOpts{
	Name: "short_url_redirects_total",
	Help: "Total number of short URL redirect responses issued.",
})

// HTTPMiddleware wraps the mux so health checks, scrapes and unmatched requests
// are included. Use the matched pattern, never a URL containing a short code.
func HTTPMiddleware(next http.Handler) http.Handler {
	return instrumentHTTP(httpDuration, next)
}

func instrumentHTTP(duration *prometheus.HistogramVec, next http.Handler) http.Handler {
	return promhttp.InstrumentHandlerDuration(duration, next,
		promhttp.WithLabelFromRequest("route", func(r *http.Request) string {
			if r.Pattern == "" {
				return "unmatched"
			}
			return r.Pattern
		}),
	)
}

// ObserveDBRequest records one SQL attempt. No rows means a lookup miss or an
// INSERT ... ON CONFLICT that did not insert; neither is a database failure.
// operation must be a fixed name, never SQL text or user input.
func ObserveDBRequest(operation string, start time.Time, err error) {
	outcome := "success"
	if errors.Is(err, sql.ErrNoRows) {
		outcome = "no_rows"
	} else if err != nil {
		outcome = "error"
	}
	dbDuration.WithLabelValues(operation, outcome).Observe(time.Since(start).Seconds())
}
