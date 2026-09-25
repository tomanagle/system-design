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

// Metrics are created at startup and passed to the components that record them.
type Metrics struct {
	httpDuration      *prometheus.HistogramVec
	ShortURLRedirects prometheus.Counter
	Store             Store
	Producer          Producer
	Analytics         Analytics
}

type Store struct {
	duration *prometheus.HistogramVec
	Created  prometheus.Counter
}

type Producer struct {
	Enqueued        prometheus.Counter
	Published       prometheus.Counter
	Dropped         *prometheus.CounterVec
	PublishErrors   prometheus.Counter
	PublishDuration *prometheus.HistogramVec
}

type Analytics struct {
	Stored       prometheus.Counter
	StoreErrors  prometheus.Counter
	CommitErrors prometheus.Counter
	Invalid      prometheus.Counter
	EventAge     prometheus.Histogram
}

// MustNew registers collectors in the supplied registry. Call once at startup.
func MustNew(registry prometheus.Registerer) *Metrics {
	factory := promauto.With(registry)
	m := &Metrics{}
	m.httpDuration = factory.NewHistogramVec(prometheus.HistogramOpts{
		Name: "http_request_duration_seconds",
		Help: "Duration of completed HTTP requests, including writing the response.",
	}, []string{"route", "method", "code"})

	m.Store.duration = factory.NewHistogramVec(prometheus.HistogramOpts{
		Name: "db_request_duration_seconds",
		Help: "Duration of database store operations, including code generation, pool wait and result scanning.",
	}, []string{"operation", "outcome"})

	m.Store.Created = factory.NewCounter(prometheus.CounterOpts{
		Name: "short_urls_created_total",
		Help: "Total number of successfully created short URLs.",
	})

	m.ShortURLRedirects = factory.NewCounter(prometheus.CounterOpts{
		Name: "short_url_redirects_total",
		Help: "Total number of short URL redirect responses issued.",
	})

	m.Producer.Enqueued = factory.NewCounter(prometheus.CounterOpts{Name: "stream_producer_enqueued_messages_total", Help: "Messages accepted into the producer queue."})
	m.Producer.Published = factory.NewCounter(prometheus.CounterOpts{Name: "stream_producer_published_messages_total", Help: "Messages acknowledged by Kafka; retries can produce duplicates."})
	m.Producer.Dropped = factory.NewCounterVec(prometheus.CounterOpts{Name: "stream_producer_dropped_messages_total", Help: "Messages not queued or discarded during shutdown."}, []string{"reason"})
	m.Producer.PublishErrors = factory.NewCounter(prometheus.CounterOpts{Name: "stream_producer_publish_errors_total", Help: "Failed Kafka publish attempts."})
	m.Analytics.Stored = factory.NewCounter(prometheus.CounterOpts{Name: "analytics_events_stored_total", Help: "Event rows stored by workers, including replayed rows."})
	m.Analytics.StoreErrors = factory.NewCounter(prometheus.CounterOpts{Name: "analytics_store_errors_total", Help: "Failed ClickHouse insert attempts."})
	m.Analytics.CommitErrors = factory.NewCounter(prometheus.CounterOpts{Name: "analytics_commit_errors_total", Help: "Failed Kafka offset commit attempts."})
	m.Analytics.Invalid = factory.NewCounter(prometheus.CounterOpts{Name: "analytics_invalid_events_total", Help: "Invalid events that stopped a worker without committing offsets."})

	m.Producer.PublishDuration = factory.NewHistogramVec(prometheus.HistogramOpts{
		Name: "stream_producer_publish_duration_seconds",
		Help: "Duration of Kafka batch write attempts, including internal writer retries, excluding queue wait.",
	}, []string{"outcome"})

	m.Analytics.EventAge = factory.NewHistogram(prometheus.HistogramOpts{
		Name: "analytics_event_age_seconds",
		Help: "Age of events when successfully stored, including queueing and retries; replays are observed again.",
	})

	return m
}

// MustRegisterProducerQueue reads the channel length at scrape time, avoiding stale
// gauge updates from concurrent enqueue/dequeue operations. Call once per app.
func MustRegisterProducerQueue(registry prometheus.Registerer, depth func() int, capacity int) {
	promauto.With(registry).NewGaugeFunc(prometheus.GaugeOpts{
		Name: "stream_producer_queue_depth",
		Help: "Messages waiting in the producer queue, excluding the batch being published.",
	}, func() float64 { return float64(depth()) })
	promauto.With(registry).NewGaugeFunc(prometheus.GaugeOpts{
		Name: "stream_producer_queue_capacity",
		Help: "Maximum number of messages waiting in the producer queue.",
	}, func() float64 { return float64(capacity) })
}

// HTTPMiddleware wraps the mux so health checks, scrapes and unmatched requests
// are included. Use the matched pattern, never a URL containing a short code.
func (m *Metrics) HTTPMiddleware(next http.Handler) http.Handler {
	return instrumentHTTP(m.httpDuration, next)
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
func (m Store) ObserveDBRequest(operation string, start time.Time, err error) {
	outcome := "success"
	if errors.Is(err, sql.ErrNoRows) {
		outcome = "no_rows"
	} else if err != nil {
		outcome = "error"
	}
	m.duration.WithLabelValues(operation, outcome).Observe(time.Since(start).Seconds())
}
