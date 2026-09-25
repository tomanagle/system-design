package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"url-shortener/internal/store"
	storemock "url-shortener/internal/store/mock"

	"github.com/stretchr/testify/mock"
)

func TestPostShortURLHandler(t *testing.T) {
	const validBody = `{"url":"https://example.com"}`
	tests := []struct {
		name           string
		body           string
		conflicts      int
		storeError     error
		cancelOnCreate bool
		wantStatus     int
		wantCalls      int
	}{
		{
			name: "created", body: validBody,
			wantStatus: http.StatusCreated, wantCalls: 1,
		},
		{
			name: "malformed JSON", body: `{"url":`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "empty body", body: "",
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "missing URL", body: `{}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "blank URL", body: `{"url":"  "}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "database failure", body: validBody,
			storeError: errors.New("private database error"),
			wantStatus: http.StatusInternalServerError, wantCalls: 1,
		},
		{
			name: "retry succeeds", body: validBody, conflicts: 2,
			wantStatus: http.StatusCreated, wantCalls: 3,
		},
		{
			name: "last retry succeeds", body: validBody, conflicts: 10,
			wantStatus: http.StatusCreated, wantCalls: 11,
		},
		{
			name: "retries exhausted", body: validBody, conflicts: 11,
			wantStatus: http.StatusInternalServerError, wantCalls: 11,
		},
		{
			name: "other error after collision", body: validBody, conflicts: 1,
			storeError: errors.New("database unavailable"),
			wantStatus: http.StatusInternalServerError, wantCalls: 2,
		},
		{
			name: "cancelled after collision", body: validBody, conflicts: 1,
			cancelOnCreate: true,
			wantStatus:     http.StatusInternalServerError, wantCalls: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req := httptest.NewRequest(http.MethodPost, "/short-url", strings.NewReader(tc.body)).WithContext(ctx)
			want := store.ShortURL{ID: 1, Code: "abc123", URL: "https://example.com"}
			mockStore := &storemock.ShortURLStoreMock{}
			mockStore.Test(t)
			t.Cleanup(func() { mockStore.AssertExpectations(t) })
			for attempt := 0; attempt < tc.wantCalls; attempt++ {
				expectation := mockStore.On("CreateShortURL", ctx, want.URL).Once()
				switch {
				case attempt < tc.conflicts:
					expectation.Return(nil, fmt.Errorf("insert: %w", store.ErrCodeConflict))
				case tc.storeError != nil:
					expectation.Return(nil, tc.storeError)
				default:
					expectation.Return(&want, nil)
				}
				if tc.cancelOnCreate {
					expectation.Run(func(mock.Arguments) { cancel() })
				}
			}

			response := httptest.NewRecorder()
			NewPostShortURLHandler(PostShortURLHandlerParams{ShortURLStore: mockStore}).ServeHTTP(response, req)
			mockStore.AssertNumberOfCalls(t, "CreateShortURL", tc.wantCalls)
			mockStore.AssertNumberOfCalls(t, "GetShortURL", 0)
			if response.Code != tc.wantStatus {
				t.Fatalf("status=%d; want status=%d", response.Code, tc.wantStatus)
			}
			if tc.wantStatus == http.StatusCreated {
				var got store.ShortURL
				if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil || got != want {
					t.Fatalf("response=%+v, err=%v; want %+v", got, err, want)
				}
				if response.Header().Get("Content-Type") != "application/json" {
					t.Error("missing JSON content type")
				}
			}
			if tc.storeError != nil && strings.Contains(response.Body.String(), tc.storeError.Error()) {
				t.Error("response exposes the database error")
			}
		})
	}
}
