package pgstore

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"url-shortener/internal/db"
	"url-shortener/internal/metrics"
	"url-shortener/internal/shortcode/shortcodetest"
	"url-shortener/internal/store"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestCreateShortURLWithMockGenerator(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run Postgres integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	// One connection keeps the temporary table local to this test's session.
	pool.SetMaxOpenConns(1)
	if _, err := pool.ExecContext(ctx, `CREATE TEMP TABLE short_url (
		id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
		code TEXT NOT NULL UNIQUE,
		url TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}

	m := metrics.MustNew(prometheus.NewRegistry())
	mockGenerator := shortcodetest.NewGenerator("abc123")
	createdCount := func() float64 {
		t.Helper()
		var metric dto.Metric
		if err := m.Store.Created.Write(&metric); err != nil {
			t.Fatal(err)
		}
		return metric.GetCounter().GetValue()
	}
	before := createdCount()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := NewShortUrlStore(ShortUrlStoreParams{Metrics: m.Store, DB: pool, CodeGenerator: mockGenerator.Generate, Logger: logger})
	created, err := s.CreateShortURL(ctx, "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 || created.Code != "abc123" || created.URL != "https://example.com" {
		t.Fatalf("unexpected short URL: %+v", created)
	}
	mockGenerator.AssertCalls(t, 1)
	saved, err := s.GetShortURL(ctx, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if saved == nil || *saved != *created {
		t.Fatalf("saved row %+v differs from returned row %+v", saved, created)
	}

	// A duplicate code must surface an error, without overwriting the original.
	if _, err := s.CreateShortURL(ctx, "https://example.org"); !errors.Is(err, store.ErrCodeConflict) {
		t.Fatalf("expected a code conflict, got %v", err)
	}
	mockGenerator.AssertCalls(t, 2)
	saved, err = s.GetShortURL(ctx, "abc123")
	if err != nil || saved == nil || saved.URL != "https://example.com" {
		t.Fatalf("duplicate insert changed the original: row=%+v, err=%v", saved, err)
	}

	// Each attempt invokes the generator again, allowing the next code to succeed.
	retryGenerator := shortcodetest.NewGenerator("abc123", "freshCode")
	retryingStore := NewShortUrlStore(ShortUrlStoreParams{Metrics: m.Store, DB: pool, CodeGenerator: retryGenerator.Generate, Logger: logger})
	if _, err := retryingStore.CreateShortURL(ctx, "https://example.org"); !errors.Is(err, store.ErrCodeConflict) {
		t.Fatalf("expected collision on first attempt, got %v", err)
	}
	created, err = retryingStore.CreateShortURL(ctx, "https://example.org")
	if err != nil || created == nil || created.Code != "freshCode" {
		t.Fatalf("fresh-code retry: row=%+v, err=%v", created, err)
	}
	retryGenerator.AssertCalls(t, 2)

	// An unrelated unique constraint must not be classified as a code collision.
	if _, err := pool.ExecContext(ctx, "ALTER TABLE short_url ADD CONSTRAINT test_url_unique UNIQUE (url)"); err != nil {
		t.Fatal(err)
	}
	otherGenerator := shortcodetest.NewGenerator("unusedCode")
	otherConflictStore := NewShortUrlStore(ShortUrlStoreParams{Metrics: m.Store, DB: pool, CodeGenerator: otherGenerator.Generate, Logger: logger})
	if _, err := otherConflictStore.CreateShortURL(ctx, "https://example.com"); err == nil || errors.Is(err, store.ErrCodeConflict) {
		t.Fatalf("expected an ordinary database error for the URL constraint, got %v", err)
	}
	otherGenerator.AssertCalls(t, 1)

	// Only the two successful inserts count; lookups, collisions and errors do not.
	if got := createdCount() - before; got != 2 {
		t.Fatalf("short URLs created counter increased by %v, want 2", got)
	}
}
