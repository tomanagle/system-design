package metrics

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestHTTPMiddleware(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := MustNew(registry)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{code}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://example.com")
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.Handle("GET /metrics", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	handler := m.HTTPMiddleware(mux)
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/abc123", 302},
		{"GET", "/different?tracking=value", 302},
		{"GET", "/healthz", 200},
		{"GET", "/metrics", 200},
		{"GET", "/not/a/route", 404},
		{"POST", "/abc123", 405},
	} {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest(tc.method, tc.path, nil))
		if r.Code != tc.status {
			t.Fatalf("%s %s: status = %d, want %d", tc.method, tc.path, r.Code, tc.status)
		}
		if tc.status == 302 && r.Header().Get("Location") != "https://example.com" {
			t.Fatal("middleware changed Location header")
		}
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]uint64{"GET /{code}|get|302": 2, "GET /healthz|get|200": 1, "GET /metrics|get|200": 1, "unmatched|get|404": 1, "unmatched|post|405": 1}
	for _, family := range families {
		if family.GetName() != "http_request_duration_seconds" {
			continue
		}
		for _, metric := range family.Metric {
			labels := map[string]string{}
			for _, label := range metric.Label {
				labels[label.GetName()] = label.GetValue()
			}
			key := labels["route"] + "|" + labels["method"] + "|" + labels["code"]
			if count, ok := want[key]; !ok || metric.Histogram.GetSampleCount() != count {
				t.Errorf("unexpected histogram %s: count %d", key, metric.Histogram.GetSampleCount())
			}
			delete(want, key)
		}
	}
	if len(want) != 0 {
		t.Errorf("missing series: %v", want)
	}
}

func TestObserveDBRequest(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		outcome string
	}{
		{"success", nil, "success"},
		{"missing", sql.ErrNoRows, "no_rows"},
		{"wrapped_missing", fmt.Errorf("scan: %w", sql.ErrNoRows), "no_rows"},
		{"failure", errors.New("database unavailable"), "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			registry := prometheus.NewRegistry()
			m := MustNew(registry)
			operation := "test_" + tc.name
			m.Store.ObserveDBRequest(operation, time.Now().Add(-time.Millisecond), tc.err)
			families, err := registry.Gather()
			if err != nil {
				t.Fatal(err)
			}
			var observations []*dto.Metric
			for _, family := range families {
				if family.GetName() == "db_request_duration_seconds" {
					observations = family.Metric
				}
			}
			if len(observations) != 1 {
				t.Fatalf("unexpected metrics: %v", families)
			}
			metric := observations[0]
			labels := map[string]string{}
			for _, label := range metric.Label {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["outcome"] != tc.outcome || metric.Histogram.GetSampleCount() != 1 || metric.Histogram.GetSampleSum() < 0.001 {
				t.Fatalf("unexpected observation: %v", metric)
			}
		})
	}
}
