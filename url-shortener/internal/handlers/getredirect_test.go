package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"url-shortener/internal/metrics"
	"url-shortener/internal/store"
	storemock "url-shortener/internal/store/mock"
	"url-shortener/internal/stream"
	streammock "url-shortener/internal/stream/mock"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestGetRedirectHandler(t *testing.T) {
	for _, tc := range []struct {
		name        string
		shortURL    *store.ShortURL
		err         error
		wantStatus  int
		wantCount   float64
		encodeError bool
	}{
		{name: "redirect", shortURL: &store.ShortURL{Code: "abc123", URL: "https://example.com"}, wantStatus: http.StatusFound, wantCount: 1},
		{name: "encoding failure still redirects", shortURL: &store.ShortURL{Code: "abc123", URL: "https://example.com"}, wantStatus: http.StatusFound, wantCount: 1, encodeError: true},
		{name: "not found", wantStatus: http.StatusNotFound},
		{name: "empty destination", shortURL: &store.ShortURL{Code: "abc123"}, wantStatus: http.StatusNotFound},
		{name: "store error", err: errors.New("database unavailable"), wantStatus: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := metrics.MustNew(prometheus.NewRegistry())
			count := func() float64 {
				t.Helper()
				var metric dto.Metric
				if err := m.ShortURLRedirects.Write(&metric); err != nil {
					t.Fatal(err)
				}
				return metric.GetCounter().GetValue()
			}
			req := httptest.NewRequest(http.MethodGet, "/abc123", nil)
			req.Header.Set("User-Agent", "test browser")
			req.Header.Set("Accept-Language", "en")
			event := stream.Event{Version: 1, EventID: "fixed-event-id", ShortCode: "abc123", UserID: "fixed-user-id", OccurredAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
			if tc.encodeError {
				event.OccurredAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
			}
			factory := &streammock.EventFactoryMock{}
			factory.Test(t)
			t.Cleanup(func() { factory.AssertExpectations(t) })
			if tc.wantStatus == http.StatusFound {
				factory.On("NewEvent", "abc123", "test browser", "en").Return(event).Once()
			}
			var logs bytes.Buffer
			events := &streammock.ProducerMock{}
			events.Test(t)
			t.Cleanup(func() { events.AssertExpectations(t) })
			wantPublishes := 0
			if tc.wantStatus == http.StatusFound && !tc.encodeError {
				wantPublishes = 1
				payload, err := json.Marshal(event)
				if err != nil {
					t.Fatal(err)
				}
				events.On("Produce", stream.Message{Key: []byte("abc123"), Value: payload}).Return(false).Once() // A full queue must not change the redirect response.
			}
			mockStore := &storemock.ShortURLStoreMock{}
			mockStore.Test(t)
			t.Cleanup(func() { mockStore.AssertExpectations(t) })
			mockStore.On("GetShortURL", req.Context(), "abc123").Return(tc.shortURL, tc.err).Once()
			before := count()
			response := httptest.NewRecorder()
			NewGetRedirectHandler(GetRedirectParams{Redirects: m.ShortURLRedirects, ShortURLStore: mockStore, Producer: events, EventFactory: factory, Logger: slog.New(slog.NewTextHandler(&logs, nil))}).ServeHTTP(response, req)
			events.AssertNumberOfCalls(t, "Produce", wantPublishes)
			factory.AssertNumberOfCalls(t, "NewEvent", int(tc.wantCount))
			if tc.encodeError && !bytes.Contains(logs.Bytes(), []byte("encode redirect event")) {
				t.Error("encoding failure must be logged")
			}
			if response.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tc.wantStatus)
			}
			if got := count() - before; got != tc.wantCount {
				t.Errorf("counter increased by %v, want %v", got, tc.wantCount)
			}
			wantLocation := ""
			if tc.wantStatus == http.StatusFound {
				wantLocation = tc.shortURL.URL
			}
			if got := response.Header().Get("Location"); got != wantLocation {
				t.Errorf("Location = %q, want %q", got, wantLocation)
			}
		})
	}
}
