package activitylog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecorderAppendsStructuredEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "activity", "events.jsonl")
	recorder, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = recorder.Close() })

	when := time.Date(
		2026, time.July, 25, 12, 30, 0, 0,
		time.FixedZone("test", -7*60*60),
	)
	if err := recorder.Record(Event{
		Timestamp: when,
		Type:      "chat",
		PlayerID:  "player-1",
		Name:      "Ryo",
		WorldID:   "dobuita",
		Text:      "Hello.",
	}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Record(Event{Type: "disconnect"}); err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var events []Event
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected two events, got %#v", events)
	}
	if events[0].Timestamp != when.UTC() ||
		events[0].Type != "chat" ||
		events[0].Text != "Hello." {
		t.Fatalf("unexpected first event: %#v", events[0])
	}
	if events[1].Timestamp.IsZero() {
		t.Fatal("recorder did not supply a timestamp")
	}
}
