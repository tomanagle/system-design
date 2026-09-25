package handlers

import (
	"net/http"

	"url-shortener/internal/metrics"
	"url-shortener/internal/store"
)

type GetRedirectHandler struct {
	shortURLStore store.ShortURLStore
}

type GetRedirectParams struct {
	ShortURLStore store.ShortURLStore
}

func NewGetRedirectHandler(params GetRedirectParams) *GetRedirectHandler {
	return &GetRedirectHandler{
		shortURLStore: params.ShortURLStore,
	}
}

func (h *GetRedirectHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	code := r.URL.Path[1:]

	shortUrl, err := h.shortURLStore.GetShortURL(r.Context(), code)

	if err != nil || shortUrl == nil || shortUrl.URL == "" {
		http.NotFound(w, r)
		return
	}

	// return a 302 temporary redirect so the browser doesn't cache the redirect permanently
	http.Redirect(w, r, shortUrl.URL, http.StatusFound)
	metrics.ShortURLRedirects.Inc()
}
