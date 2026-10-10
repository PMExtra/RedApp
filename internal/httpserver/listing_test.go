package httpserver

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/PMExtra/RedApp/internal/apps/builtin"
	"github.com/PMExtra/RedApp/internal/store"
)

func listingServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	registry, err := builtin.New()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB.Close() })
	return &Server{store: db, registry: registry}, db
}
func listRequest(s *Server, app, endpoint, query string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "http://internal/list"+query, nil)
	s.eventList(w, r, app)
	return w
}
func decodeList[T any](t *testing.T, w *httptest.ResponseRecorder) listPage[T] {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("list status %d: %s", w.Code, w.Body.String())
	}
	var page listPage[T]
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Items == nil {
		t.Fatal("empty items must be an array")
	}
	return page
}

func TestEventPaginationIsBoundedAndScoped(t *testing.T) {
	s, db := listingServer(t)
	app, other := "openai/codex", "anthropic/claude-code"
	for i := 0; i < 5; i++ {
		owner := app
		if i%2 == 0 {
			owner = other
		}
		if err := db.RecordEvent(store.Event{AppID: owner, Version: "1.0.0", Category: "metadata", Code: "metadata_fetch_failed", Message: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	events := decodeList[store.ListedEvent](t, listRequest(s, "", "events", "?limit=2"))
	if len(events.Items) != 2 || events.Items[0].ID <= events.Items[1].ID || events.NextCursor == nil {
		t.Fatal("events are not bounded in descending id order")
	}
	eventCursor := *events.NextCursor
	next := decodeList[store.ListedEvent](t, listRequest(s, "", "events", "?limit=2&cursor="+eventCursor))
	if len(next.Items) != 2 || next.Items[0].ID >= events.Items[1].ID {
		t.Fatal("event page repeated rows")
	}
	owned := decodeList[store.ListedEvent](t, listRequest(s, app, "events", "?limit=1"))
	if len(owned.Items) != 1 || owned.Items[0].AppID != app || owned.NextCursor == nil {
		t.Fatal("event app filter missing")
	}
	owned = decodeList[store.ListedEvent](t, listRequest(s, app, "events", "?limit=1&cursor="+*owned.NextCursor))
	if len(owned.Items) != 1 || owned.Items[0].AppID != app || owned.NextCursor != nil {
		t.Fatal("event app continuation crossed scope")
	}
	if w := listRequest(s, app, "events", "?cursor="+eventCursor); w.Code != 400 {
		t.Fatal("global event cursor used for application")
	}
	for _, query := range []string{"?limit=0", "?limit=101", "?limit=01", "?limit=2&limit=3", "?cursor=", "?cursor=not-base64", "?version=1.0.0", "?app=" + url.QueryEscape(other), "?limit=1;bad=1"} {
		if w := listRequest(s, "", "events", query); w.Code != 400 {
			t.Fatalf("query %q accepted: %d", query, w.Code)
		}
	}
	if err := db.DB.Close(); err != nil {
		t.Fatal(err)
	}
	if w := listRequest(s, app, "events", ""); w.Code != 503 {
		t.Fatalf("database failure returned %d", w.Code)
	}
}
