package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"

	"url-shortener/internal/store"
	"url-shortener/internal/stream"
)

type GetRedirectHandler struct {
	shortURLStore store.ShortURLStore
	producer      stream.Producer
	eventFactory  stream.EventFactory
	logger        *slog.Logger
	redirects     prometheus.Counter
}

type GetRedirectParams struct {
	ShortURLStore store.ShortURLStore
	Producer      stream.Producer
	EventFactory  stream.EventFactory
	Logger        *slog.Logger
	Redirects     prometheus.Counter
}

func NewGetRedirectHandler(params GetRedirectParams) *GetRedirectHandler {
	return &GetRedirectHandler{
		shortURLStore: params.ShortURLStore,
		producer:      params.Producer,
		eventFactory:  params.EventFactory,
		logger:        params.Logger,
		redirects:     params.Redirects,
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

	h.redirects.Inc()

	event := h.eventFactory.NewEvent(code, r.UserAgent(), r.Header.Get("Accept-Language"))

	payload, err := json.Marshal(event)

	if err != nil {
		h.logger.ErrorContext(r.Context(), "encode redirect event", "error", err)
		return
	}

	h.producer.Produce(stream.Message{
		Key:   []byte(event.PartitionKey()),
		Value: payload,
	})
}
