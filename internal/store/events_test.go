package store

import "testing"

func TestStructuredEventsKeepExplicitStatusAndApplicationScope(t *testing.T) {
	s := openTest(t)
	status := 502
	for _, app := range []string{storageOf(t, s, "openai/codex"), storageOf(t, s, "anthropic/claude-code")} {
		if err := s.RecordEvent(Event{AppID: app, Version: "1.0.0", ResourceKey: "binary", Category: "http", Code: "upstream_http", Message: "The wording may change", UpstreamStatus: &status}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := s.EventsFor(storageOf(t, s, "openai/codex"))
	if err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
	if events[0]["status_code"] != 502 || events[0]["app_id"] != storageOf(t, s, "openai/codex") {
		t.Fatal(events)
	}
	all, err := s.Events()
	if err != nil || len(all) != 2 {
		t.Fatal(all, err)
	}
}
