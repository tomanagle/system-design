package pgstore

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"url-shortener/internal/metrics"
	"url-shortener/internal/store"
)

type ShortUrlStore struct {
	db            *sql.DB
	codeGenerator func() string
	logger        *slog.Logger
	metrics       metrics.Store
}

type ShortUrlStoreParams struct {
	DB            *sql.DB
	CodeGenerator func() string
	Logger        *slog.Logger
	Metrics       metrics.Store
}

// NewShortUrlStore requires all dependencies to be supplied by the caller.
func NewShortUrlStore(params ShortUrlStoreParams) *ShortUrlStore {
	return &ShortUrlStore{
		db:            params.DB,
		codeGenerator: params.CodeGenerator,
		logger:        params.Logger,
		metrics:       params.Metrics,
	}
}

func (s *ShortUrlStore) CreateShortURL(ctx context.Context, url string) (*store.ShortURL, error) {

	var err error
	start := time.Now()
	code := s.codeGenerator()
	defer func() {
		s.logger.DebugContext(ctx, "CreateShortURL", "duration", time.Since(start), "code", code, "success", err == nil)
		s.metrics.ObserveDBRequest("create_short_url", start, err)
	}()

	newShortURL := &store.ShortURL{
		Code: code,
		URL:  url,
	}

	err = s.db.QueryRowContext(ctx,
		"INSERT INTO short_url (code, url) VALUES ($1, $2) ON CONFLICT (code) DO NOTHING RETURNING id",
		newShortURL.Code, newShortURL.URL,
	).Scan(&newShortURL.ID)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrCodeConflict
	}
	if err != nil {
		return nil, err
	}

	s.metrics.Created.Inc()

	return newShortURL, nil
}

func (s *ShortUrlStore) GetShortURL(ctx context.Context, code string) (*store.ShortURL, error) {
	var err error
	start := time.Now()
	defer func() {
		s.logger.DebugContext(ctx, "GetShortURL", "duration", time.Since(start), "code", code, "success", err == nil)
		s.metrics.ObserveDBRequest("get_short_url", start, err)
	}()

	row := s.db.QueryRowContext(ctx, "SELECT id, code, url FROM short_url WHERE code = $1", code)
	var shortURL store.ShortURL
	err = row.Scan(&shortURL.ID, &shortURL.Code, &shortURL.URL)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	return &shortURL, nil
}
