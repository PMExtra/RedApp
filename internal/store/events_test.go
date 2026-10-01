package store

import "testing"

func TestEventHTTPStatusPreservesEnglishAndLegacyHistory(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	messages := map[string]int{"Upstream HTTP 502": 502, "\u4e0a\u6e38 HTTP 503": 503, "Disk write failed": 0}
	for message := range messages {
		if err := db.Event("test", "http", message); err != nil {
			t.Fatal(err)
		}
	}
	events, err := db.Events()
	if err != nil || len(events) != len(messages) {
		t.Fatalf("events: %v count=%d", err, len(events))
	}
	for _, event := range events {
		message := event["message"].(string)
		want, ok := messages[message]
		if !ok || event["status_code"] != want {
			t.Fatalf("event history or status changed: %v", event)
		}
	}
}
