package kafkastream

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"time"

	"url-shortener/internal/metrics"
	"url-shortener/internal/stream"

	"github.com/segmentio/kafka-go"
)

const publishBatchSize = 100

type MessageWriter interface {
	WriteMessages(context.Context, ...kafka.Message) error
	Close() error
}

type ProducerParams struct {
	Writer    MessageWriter
	Logger    *slog.Logger
	QueueSize int
	Metrics   metrics.Producer
}

type Producer struct {
	writer  MessageWriter
	metrics metrics.Producer
	logger  *slog.Logger
	queue   chan stream.Message
	done    chan struct{}
	cancel  context.CancelFunc
	mu      sync.RWMutex
	closed  bool
}

func NewProducer(params ProducerParams) *Producer {
	ctx, cancel := context.WithCancel(context.Background())

	p := &Producer{
		writer:  params.Writer,
		metrics: params.Metrics,
		logger:  params.Logger,
		queue:   make(chan stream.Message, params.QueueSize),
		done:    make(chan struct{}),
		cancel:  cancel,
	}

	go p.run(ctx)

	return p
}

func (p *Producer) QueueDepth() int {
	return len(p.queue)
}

// QueueCapacity reports the channel's own capacity, so the exported metric
// cannot disagree with the queue it describes.
func (p *Producer) QueueCapacity() int {
	return cap(p.queue)
}

// Produce never waits for Kafka or space in the queue.
// It copies the key and payload so the caller can reuse its buffers after return.
func (p *Producer) Produce(message stream.Message) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		p.metrics.Dropped.WithLabelValues("shutdown").Inc()
		return false
	}
	select {
	case p.queue <- stream.Message{Key: bytes.Clone(message.Key), Value: bytes.Clone(message.Value)}:
		p.metrics.Enqueued.Inc()
		return true
	default:
		p.metrics.Dropped.WithLabelValues("queue_full").Inc()
		return false
	}
}

// Close stops intake and gives queued events until ctx expires to reach Kafka.
func (p *Producer) Close(ctx context.Context) {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.queue)
	}
	p.mu.Unlock()
	select {
	case <-p.done:
	case <-ctx.Done():
		p.logger.Warn("producer drain timed out; remaining messages may be lost")
	}
	p.cancel()
}

func (p *Producer) run(ctx context.Context) {
	defer close(p.done)
	defer func() {
		if err := p.writer.Close(); err != nil {
			p.logger.Warn("close Kafka writer", "error", err)
		}
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	batch := make([]kafka.Message, 0, publishBatchSize)
	defer func() {
		p.metrics.Dropped.WithLabelValues("shutdown").Add(float64(len(batch) + len(p.queue)))
	}()

	flush := func() bool {
		if len(batch) == 0 {
			return true
		}
		published := retry(ctx, p.logger, "publish message batch; will retry", p.metrics.PublishErrors, func() error {
			start := time.Now()
			err := p.writer.WriteMessages(ctx, batch...)
			outcome := "success"
			if err != nil {
				outcome = "error"
			}
			p.metrics.PublishDuration.WithLabelValues(outcome).Observe(time.Since(start).Seconds())
			return err
		})
		if !published {
			return false
		}
		p.metrics.Published.Add(float64(len(batch)))
		batch = batch[:0]
		return true
	}

	for {
		select {
		case <-ctx.Done():
			return
		case message, ok := <-p.queue:
			if !ok {
				flush()
				return
			}
			batch = append(batch, kafka.Message{Key: message.Key, Value: message.Value})
			if len(batch) == cap(batch) && !flush() {
				return
			}
		case <-ticker.C:
			if !flush() {
				return
			}
		}
	}
}
