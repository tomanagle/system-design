package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"url-shortener/internal/store"
)

const maxCodeRetries = 10

type PostShortURLHandler struct {
	urlShortenerStore store.ShortURLStore
}

type PostShortURLHandlerParams struct {
	ShortURLStore store.ShortURLStore
}

func NewPostShortURLHandler(params PostShortURLHandlerParams) *PostShortURLHandler {
	return &PostShortURLHandler{
		urlShortenerStore: params.ShortURLStore,
	}
}

type PostShortURLParams struct {
	URL string `json:"url"`
}

func (h *PostShortURLHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var params PostShortURLParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(params.URL) == "" {
		http.Error(w, "URL is required", http.StatusBadRequest)
		return
	}

	var shortURL *store.ShortURL
	var err error
	// One initial attempt, followed by at most ten retries with fresh codes.
	for attempt := 0; attempt <= maxCodeRetries; attempt++ {
		if err = r.Context().Err(); err != nil {
			break
		}
		shortURL, err = h.urlShortenerStore.CreateShortURL(r.Context(), params.URL)
		if !errors.Is(err, store.ErrCodeConflict) {
			break
		}
	}
	if err != nil {
		http.Error(w, "Could not create short URL", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(shortURL)
}
