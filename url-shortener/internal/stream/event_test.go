package stream

import (
	"encoding/json"
	"testing"
)

func TestEventIdentity(t *testing.T) {
	first := NewEventGenerator([]byte("secret")).NewEvent("abc1234", "browser", "en")
	second := NewEventGenerator([]byte("secret")).NewEvent("abc1234", "browser", "en")
	if first.EventID == second.EventID || first.UserID != second.UserID || len(first.UserID) != 64 {
		t.Fatal("event ID or pseudonymous user ID is incorrect")
	}
	if first.UserID == NewEventGenerator([]byte("other-key")).NewEvent("abc1234", "browser", "en").UserID {
		t.Fatal("fingerprint must depend on key")
	}
	if first.UserID == NewEventGenerator([]byte("secret")).NewEvent("abc1234", "different-browser", "en").UserID {
		t.Fatal("fingerprint must depend on signature")
	}
	data, _ := json.Marshal(first)
	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil || decoded != first {
		t.Fatalf("event did not round trip: %s, %v", data, err)
	}
}
