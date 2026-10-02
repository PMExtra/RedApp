package store

import "testing"

func TestStructuredEventsKeepExplicitStatusAndApplicationScope(t *testing.T) {
	s := openTest(t)
	status := 502
	for _, app := range []string{"openai/codex", "anthropic/claude-code"} {
		if err := s.RecordEvent(Event{AppID: app, Version: "1.0.0", ResourceKey: "binary", Category: "http", Code: "upstream_http", Message: "The wording may change", UpstreamStatus: &status}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := s.EventsFor("openai/codex")
	if err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
	if events[0]["status_code"] != 502 || events[0]["app_id"] != "openai/codex" {
		t.Fatal(events)
	}
	all, err := s.Events()
	if err != nil || len(all) != 2 {
		t.Fatal(all, err)
	}
}
