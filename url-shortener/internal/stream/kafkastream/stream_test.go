package kafkastream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"url-shortener/internal/metrics"
	"url-shortener/internal/stream"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/segmentio/kafka-go"
)

func TestPartition(t *testing.T) {
	a, b := NewKafkaWriter([]string{"localhost:9092"}, "test"), NewKafkaWriter([]string{"localhost:9092"}, "test")
	defer a.Close()
	defer b.Close()
	message := kafka.Message{Key: []byte("abc1234")}
	if a.Balancer.Balance(message, 0, 1, 2, 3) != b.Balancer.Balance(message, 0, 1, 2, 3) {
		t.Fatal("different producers must agree on the partition")
	}
}

type testWriter struct {
	write func(context.Context, ...kafka.Message) error
}

func (w testWriter) WriteMessages(ctx context.Context, messages ...kafka.Message) error {
	return w.write(ctx, messages...)
}
func (w testWriter) Close() error { return nil }
func quietLogger() *slog.Logger   { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestProducerQueueNeverWaitsForKafka(t *testing.T) {
	m := metrics.MustNew(prometheus.NewRegistry())
	entered := make(chan struct{})
	var once sync.Once
	writer := testWriter{write: func(ctx context.Context, _ ...kafka.Message) error {
		once.Do(func() { close(entered) })
		<-ctx.Done()
		return ctx.Err()
	}}
	producer := NewProducer(ProducerParams{Metrics: m.Producer, Writer: writer, Logger: quietLogger(), QueueSize: 1})
	defer producer.cancel()
	message := stream.Message{Key: []byte("any-key"), Value: []byte{0xff, 0x00, 0x01}}
	if !producer.Produce(message) {
		t.Fatal("first event rejected")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("writer did not start")
	}
	if !producer.Produce(message) {
		t.Fatal("queue should have one free slot")
	}
	if producer.QueueDepth() != 1 {
		t.Fatal("queue depth must exclude the batch blocked in the writer")
	}
	result := make(chan bool, 1)
	go func() { result <- producer.Produce(message) }()
	select {
	case accepted := <-result:
		if accepted {
			t.Fatal("full queue accepted event")
		}
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on Kafka")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	producer.Close(ctx)
	if producer.Produce(message) {
		t.Fatal("closed producer accepted an event")
	}
	select {
	case <-producer.done:
	case <-time.After(time.Second):
		t.Fatal("producer did not stop")
	}
}

func TestProducerRetriesStableMessagesAndDrains(t *testing.T) {
	m := metrics.MustNew(prometheus.NewRegistry())
	successBefore := histogramCount(t, m.Producer.PublishDuration.WithLabelValues("success").(prometheus.Metric))
	errorBefore := histogramCount(t, m.Producer.PublishDuration.WithLabelValues("error").(prometheus.Metric))
	var attempts [][]kafka.Message
	writer := testWriter{write: func(_ context.Context, messages ...kafka.Message) error {
		attempts = append(attempts, append([]kafka.Message(nil), messages...))
		if len(attempts) == 1 {
			return errors.New("Kafka unavailable")
		}
		return nil
	}}
	producer := NewProducer(ProducerParams{Metrics: m.Producer, Writer: writer, Logger: quietLogger(), QueueSize: 10})
	first := stream.Message{Key: []byte("same-key"), Value: []byte{0xff, 0x00, 0x01}}
	second := stream.Message{Key: []byte("same-key"), Value: []byte("not JSON either")}
	firstKey, firstValue := bytes.Clone(first.Key), bytes.Clone(first.Value)
	producer.Produce(first)
	producer.Produce(second)
	// Once queued, caller-owned buffers can change without corrupting delivery.
	first.Key[0] = 'X'
	first.Value[0] = 0
	first = stream.Message{Key: firstKey, Value: firstValue}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	producer.Close(ctx)
	<-producer.done
	if histogramCount(t, m.Producer.PublishDuration.WithLabelValues("success").(prometheus.Metric))-successBefore != 1 ||
		histogramCount(t, m.Producer.PublishDuration.WithLabelValues("error").(prometheus.Metric))-errorBefore != 1 {
		t.Fatal("publish latency must record the failed attempt and successful retry separately")
	}
	if len(attempts) != 2 || len(attempts[1]) != 2 {
		t.Fatalf("unexpected attempts: %v", attempts)
	}
	for i, message := range []stream.Message{first, second} {
		for _, attempt := range attempts {
			if !bytes.Equal(attempt[i].Value, message.Value) || !bytes.Equal(attempt[i].Key, message.Key) {
				t.Fatal("publish and retry must preserve arbitrary bytes, partition key, and order")
			}
		}
	}
}

type testReader struct {
	commit func(context.Context, ...kafka.Message) error
}

func (r testReader) FetchMessage(context.Context) (kafka.Message, error) { panic("not used") }
func (r testReader) CommitMessages(ctx context.Context, messages ...kafka.Message) error {
	return r.commit(ctx, messages...)
}

type testStore struct {
	insert func(context.Context, []stream.StoredEvent) error
}

func (s testStore) Insert(ctx context.Context, events []stream.StoredEvent) error {
	return s.insert(ctx, events)
}

func TestConsumerStoresBeforeCommit(t *testing.T) {
	m := metrics.MustNew(prometheus.NewRegistry())
	ageBefore := histogramCount(t, m.Analytics.EventAge)
	event := stream.NewEventGenerator([]byte("key")).NewEvent("abc1234", "browser", "en")
	data, _ := json.Marshal(event)
	batch := []kafka.Message{{Topic: "redirects", Partition: 2, Offset: 7, Key: []byte(event.ShortCode), Value: data}}
	attempts, commits := 0, 0
	worker := &Consumer{Metrics: m.Analytics, Logger: quietLogger(), Store: testStore{insert: func(_ context.Context, events []stream.StoredEvent) error {
		attempts++
		if commits != 0 {
			t.Fatal("committed before insert")
		}
		if events[0].EventID != event.EventID || events[0].KafkaOffset != 7 {
			t.Fatal("lost event or Kafka metadata")
		}
		if attempts == 1 {
			return errors.New("ClickHouse unavailable")
		}
		if histogramCount(t, m.Analytics.EventAge) != ageBefore {
			t.Fatal("failed inserts must not record event age")
		}
		return nil
	}}, Reader: testReader{commit: func(_ context.Context, messages ...kafka.Message) error {
		commits++
		if attempts != 2 || len(messages) != 1 || messages[0].Offset != 7 {
			t.Fatal("incorrect commit")
		}
		return nil
	}}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := worker.process(ctx, batch); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || commits != 1 {
		t.Fatalf("attempts=%d commits=%d", attempts, commits)
	}
	if histogramCount(t, m.Analytics.EventAge)-ageBefore != 1 {
		t.Fatal("successful storage must record event age once despite retry")
	}
}

func histogramCount(t *testing.T, metric prometheus.Metric) uint64 {
	t.Helper()
	var value dto.Metric
	if err := metric.Write(&value); err != nil {
		t.Fatal(err)
	}
	return value.GetHistogram().GetSampleCount()
}

func TestConsumerDoesNotCommitFailedOrInvalidEvents(t *testing.T) {
	m := metrics.MustNew(prometheus.NewRegistry())
	for _, invalid := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		event := stream.NewEventGenerator([]byte("key")).NewEvent("abc1234", "browser", "en")
		data, _ := json.Marshal(event)
		if invalid {
			data = []byte("invalid json")
		}
		worker := &Consumer{Metrics: m.Analytics, Logger: quietLogger(), Store: testStore{insert: func(context.Context, []stream.StoredEvent) error { cancel(); return errors.New("unavailable") }}, Reader: testReader{commit: func(context.Context, ...kafka.Message) error { t.Fatal("must not commit"); return nil }}}
		err := worker.process(ctx, []kafka.Message{{Key: []byte(event.ShortCode), Value: data}})
		cancel()
		if invalid && err == nil {
			t.Fatal("invalid event should fail closed")
		}
	}
}
