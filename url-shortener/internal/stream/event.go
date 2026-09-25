package stream

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Event struct {
	Version    int       `json:"version"`
	EventID    string    `json:"event_id"`
	ShortCode  string    `json:"short_code"`
	OccurredAt time.Time `json:"occurred_at"`
	UserID     string    `json:"user_id"`
}

type EventGenerator struct {
	fingerprintKey []byte
}

func NewEventGenerator(fingerprintKey []byte) *EventGenerator {
	return &EventGenerator{fingerprintKey: append([]byte(nil), fingerprintKey...)}
}

// NewEvent uses a pseudonymous browser signature, not a unique person ID.
// Raw headers and IP addresses are not included in the event.
func (g *EventGenerator) NewEvent(code, userAgent, language string) Event {
	signature, _ := json.Marshal([]string{userAgent, language})
	hash := hmac.New(sha256.New, g.fingerprintKey)
	_, _ = hash.Write(signature)
	return Event{
		Version:    1,
		EventID:    rand.Text(),
		ShortCode:  code,
		OccurredAt: time.Now().UTC(),
		UserID:     hex.EncodeToString(hash.Sum(nil)),
	}
}

// PartitionKey keeps every event for one short code on a single partition,
// so a code's events stay ordered. Producers and consumers must agree on it.
func (e Event) PartitionKey() string {
	return e.ShortCode
}

// Valid reports why an event cannot be stored, so consumers reject it before
// it reaches the database rather than re-deriving the rules at each call site.
func (e Event) Valid() error {
	switch {
	case e.Version != 1:
		return fmt.Errorf("unsupported version %d", e.Version)
	case e.EventID == "":
		return errors.New("missing event ID")
	case e.ShortCode == "":
		return errors.New("missing short code")
	case e.UserID == "":
		return errors.New("missing user ID")
	case e.OccurredAt.IsZero():
		return errors.New("missing timestamp")
	}
	return nil
}

type StoredEvent struct {
	Event
	KafkaTopic     string `json:"kafka_topic"`
	KafkaPartition int    `json:"kafka_partition"`
	KafkaOffset    int64  `json:"kafka_offset"`
}
