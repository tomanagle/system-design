package store

import (
	"context"
	"errors"
)

var ErrCodeConflict = errors.New("short URL code already exists")

type ShortURL struct {
	ID   int64  `json:"id"`
	Code string `json:"code"`
	URL  string `json:"url"`
}

type ShortURLStore interface {
	CreateShortURL(ctx context.Context, url string) (*ShortURL, error)
	GetShortURL(ctx context.Context, code string) (*ShortURL, error)
}
