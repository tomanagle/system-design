package chstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"url-shortener/internal/stream"
)

type Store struct {
	URL      string
	Database string
	Username string
	Password string
	Client   *http.Client
}

func (s *Store) MustOpen(ctx context.Context) {
	if err := s.query(ctx, "SELECT event_id FROM redirect_events LIMIT 0", nil); err != nil {
		panic(fmt.Errorf("open ClickHouse analytics store: %w", err))
	}
}

func (s *Store) Insert(ctx context.Context, events []stream.StoredEvent) error {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			return err
		}
	}
	return s.query(ctx, "INSERT INTO redirect_events FORMAT JSONEachRow", &body)
}

func (s *Store) query(ctx context.Context, query string, body io.Reader) error {
	endpoint, err := url.Parse(s.URL)
	if err != nil {
		return err
	}
	params := endpoint.Query()
	params.Set("database", s.Database)
	params.Set("query", query)
	params.Set("date_time_input_format", "best_effort")
	// Inserts must be acknowledged only after storage, before Kafka offsets commit.
	params.Set("async_insert", "0")
	params.Set("wait_end_of_query", "1")
	endpoint.RawQuery = params.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), body)
	if err != nil {
		return err
	}
	request.SetBasicAuth(s.Username, s.Password)
	response, err := s.Client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	result, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("ClickHouse status %d: %s", response.StatusCode, result)
	}
	return nil
}
