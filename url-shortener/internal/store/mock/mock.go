package mock

import (
	"context"

	"url-shortener/internal/store"

	"github.com/stretchr/testify/mock"
)

type ShortURLStoreMock struct {
	mock.Mock
}

func (m *ShortURLStoreMock) CreateShortURL(ctx context.Context, url string) (*store.ShortURL, error) {
	args := m.Called(ctx, url)
	var shortURL *store.ShortURL
	if args.Get(0) != nil {
		shortURL = args.Get(0).(*store.ShortURL)
	}
	return shortURL, args.Error(1)
}

func (m *ShortURLStoreMock) GetShortURL(ctx context.Context, code string) (*store.ShortURL, error) {
	args := m.Called(ctx, code)
	var shortURL *store.ShortURL
	if args.Get(0) != nil {
		shortURL = args.Get(0).(*store.ShortURL)
	}
	return shortURL, args.Error(1)
}
