package kafkastream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"url-shortener/internal/metrics"
	"url-shortener/internal/stream"

	"github.com/segmentio/kafka-go"
)

const consumeBatchSize = 1000

type MessageReader interface {
	FetchMessage(context.Context) (kafka.Message, error)
	CommitMessages(context.Context, ...kafka.Message) error
}

type Consumer struct {
	Reader  MessageReader
	Store   stream.EventStore
	Logger  *slog.Logger
	Metrics metrics.Analytics
}

// Run processes batches sequentially and commits only after durable storage.
// Failed inserts block consumption and are retried; Kafka retains the backlog.
func (w *Consumer) Run(ctx context.Context) error {
	// One batch buffer serves every iteration; process does not retain it.
	batch := make([]kafka.Message, 0, consumeBatchSize)
	for ctx.Err() == nil {
		batch = batch[:0]
		batchCtx, cancel := context.WithTimeout(ctx, time.Second)
		for len(batch) < cap(batch) {
			message, err := w.Reader.FetchMessage(batchCtx)
			if err != nil {
				// The batch window is closing either way; the outer loop retries.
				if batchCtx.Err() == nil {
					w.Logger.Warn("fetch analytics event; will retry", "error", err)
				}
				break
			}
			batch = append(batch, message)
		}
		cancel()
		if ctx.Err() != nil {
			return nil
		}
		if len(batch) == 0 {
			continue
		}
		if err := w.process(ctx, batch); err != nil {
			return err
		}
	}
	return nil
}

func (w *Consumer) process(ctx context.Context, batch []kafka.Message) error {
	events := make([]stream.StoredEvent, 0, len(batch))
	for _, message := range batch {
		var event stream.Event
		err := json.Unmarshal(message.Value, &event)
		if err == nil {
			err = event.Valid()
		}
		if err == nil && string(message.Key) != event.PartitionKey() {
			err = errors.New("partition key does not match short code")
		}
		if err != nil {
			w.Metrics.Invalid.Inc()
			// Fail closed: never commit past a record we cannot interpret.
			return fmt.Errorf("invalid analytics event at %s/%d/%d: %w", message.Topic, message.Partition, message.Offset, err)
		}
		events = append(events, stream.StoredEvent{Event: event, KafkaTopic: message.Topic, KafkaPartition: message.Partition, KafkaOffset: message.Offset})
	}

	if !retry(ctx, w.Logger, "store analytics batch; offsets not committed", w.Metrics.StoreErrors, func() error {
		return w.Store.Insert(ctx, events)
	}) {
		return nil
	}

	w.Metrics.Stored.Add(float64(len(events)))
	storedAt := time.Now()
	for _, event := range events {
		// Producer and worker clocks can differ; never record a negative duration.
		w.Metrics.EventAge.Observe(max(0, storedAt.Sub(event.OccurredAt).Seconds()))
	}

	// Retry the commit without re-inserting. A crash before commit still replays
	// the same event IDs, which ClickHouse deduplicates at query/merge time.
	retry(ctx, w.Logger, "commit analytics offsets; will retry", w.Metrics.CommitErrors, func() error {
		return w.Reader.CommitMessages(ctx, batch...)
	})
	return nil
}
