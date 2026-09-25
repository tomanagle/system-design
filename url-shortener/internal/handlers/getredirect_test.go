package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"url-shortener/internal/metrics"
	"url-shortener/internal/store"
	storemock "url-shortener/internal/store/mock"

	dto "github.com/prometheus/client_model/go"
)

func TestGetRedirectHandler(t *testing.T) {
	for _, tc := range []struct {
		name       string
		shortURL   *store.ShortURL
		err        error
		wantStatus int
		wantCount  float64
	}{
		{name: "redirect", shortURL: &store.ShortURL{Code: "abc123", URL: "https://example.com"}, wantStatus: http.StatusFound, wantCount: 1},
		{name: "not found", wantStatus: http.StatusNotFound},
		{name: "empty destination", shortURL: &store.ShortURL{Code: "abc123"}, wantStatus: http.StatusNotFound},
		{name: "store error", err: errors.New("database unavailable"), wantStatus: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count := func() float64 {
				t.Helper()
				var metric dto.Metric
				if err := metrics.ShortURLRedirects.Write(&metric); err != nil {
					t.Fatal(err)
				}
				return metric.GetCounter().GetValue()
			}
			req := httptest.NewRequest(http.MethodGet, "/abc123", nil)
			mockStore := &storemock.ShortURLStoreMock{}
			mockStore.Test(t)
			t.Cleanup(func() { mockStore.AssertExpectations(t) })
			mockStore.On("GetShortURL", req.Context(), "abc123").Return(tc.shortURL, tc.err).Once()
			before := count()
			response := httptest.NewRecorder()
			NewGetRedirectHandler(GetRedirectParams{ShortURLStore: mockStore}).ServeHTTP(response, req)
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
