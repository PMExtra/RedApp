package httpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/application"
	"github.com/PMExtra/RedApp/internal/apps/builtin"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/download"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/testutil"
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
	return &Server{DB: db, Registry: registry}, db
}
func listRequest(s *Server, app, endpoint, query string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "http://internal/list"+query, nil)
	if endpoint == "events" {
		s.eventList(w, r, app)
	} else {
		s.applicationList(w, r, app, endpoint)
	}
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

func TestVersionAndEventPaginationAreBoundedAndScoped(t *testing.T) {
	s, db := listingServer(t)
	app, other := "openai/codex", "anthropic/claude-code"
	expected := make([]string, 0, 105)
	for i := 0; i < 105; i++ {
		v := fmt.Sprintf("1.0.%d", i)
		expected = append(expected, v)
		if err := db.SeenFor(app, v); err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(expected)
	if err := db.SeenFor(other, "9.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := db.AddVersion(app, "1.0.0", 7, 11); err != nil {
		t.Fatal(err)
	}
	if n, err := db.VersionCount(app); err != nil || n != 105 {
		t.Fatalf("count scope: %d %v", n, err)
	}
	first := decodeList[listedVersion](t, listRequest(s, app, "versions", ""))
	if len(first.Items) != 50 || first.NextCursor == nil || first.Items[0].Requests != 7 || first.Items[0].Bytes != 11 {
		t.Fatal("default bound or version statistics lost", first)
	}
	page := decodeList[listedVersion](t, listRequest(s, app, "versions", "?limit=100"))
	if len(page.Items) != 100 || page.NextCursor == nil {
		t.Fatal("maximum bound missing")
	}
	got := make([]string, 0, 105)
	for _, v := range page.Items {
		got = append(got, v.Version)
	}
	cursor := *page.NextCursor
	page = decodeList[listedVersion](t, listRequest(s, app, "versions", "?limit=100&cursor="+url.QueryEscape(cursor)))
	if len(page.Items) != 5 || page.NextCursor != nil {
		t.Fatal("last version page did not terminate")
	}
	for _, v := range page.Items {
		got = append(got, v.Version)
	}
	if fmt.Sprint(got) != fmt.Sprint(expected) {
		t.Fatal("version pagination skipped, duplicated or crossed app", got)
	}
	if w := listRequest(s, other, "versions", "?cursor="+cursor); w.Code != 400 {
		t.Fatal("cross-app cursor accepted", w.Code)
	}
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
	for _, query := range []string{"?limit=0", "?limit=101", "?limit=01", "?limit=2&limit=3", "?cursor=", "?cursor=not-base64", "?version=1.0.0", "?app=" + url.QueryEscape(other), "?limit=1;bad=1", "?cursor=" + cursor} {
		if w := listRequest(s, "", "events", query); w.Code != 400 {
			t.Fatalf("query %q accepted: %d", query, w.Code)
		}
	}
	if err := db.DB.Close(); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"versions", "events"} {
		if w := listRequest(s, app, endpoint, ""); w.Code != 503 {
			t.Fatalf("database failure returned %d", w.Code)
		}
	}
}

func TestResourcePaginationBindsVersionFilterAndApplication(t *testing.T) {
	payload := []byte("bounded listing fixture")
	digest := sha256.Sum256(payload)
	hash := hex.EncodeToString(digest[:])
	size := int64(len(payload))
	client, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(payload) }))
	s, db := listingServer(t)
	entries := s.Registry.Entries()
	clients := map[string]*distributor.Client{}
	for i := range entries {
		entries[i].Upstream = client
		clients[entries[i].Descriptor.ID] = client
	}
	var err error
	s.Registry, err = application.NewRegistry(entries)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	s.Downloads, err = download.NewApplications(dir, db, clients)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Downloads.Close() })
	app, other := "openai/codex", "anthropic/claude-code"
	for _, owner := range []string{app, other} {
		for _, version := range []string{"1.0.0", "1.0.1"} {
			resources := []store.Resource{}
			for i := 0; i < 3; i++ {
				resources = append(resources, store.Resource{AppID: owner, Version: version, Key: fmt.Sprintf("artifact-%d", i), SourceURL: client.URL("artifact"), SHA256: hash, ExpectedSize: &size})
			}
			if err := db.PutRelease(store.ReleaseMetadata{AppID: owner, Version: version, Raw: []byte("{}"), TrustRevision: 1, FetchedAt: time.Now()}, resources); err != nil {
				t.Fatal(err)
			}
			for _, bound := range resources {
				r := download.Resource{Application: owner, Version: version, Key: bound.Key, ID: download.LogicalIdentity(owner, version, bound.Key), Source: bound.SourceURL, Hash: hash, Size: &size}
				reader, _, err := s.Downloads.Acquire(context.Background(), r)
				if err != nil {
					t.Fatal(err)
				}
				_, err = io.Copy(io.Discard, reader)
				reader.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	query := "?limit=2&version=1.0.0"
	first := decodeList[download.View](t, listRequest(s, app, "resources", query))
	if len(first.Items) != 2 || first.NextCursor == nil || first.Items[0].ID >= first.Items[1].ID {
		t.Fatal("resource ordering or page limit missing")
	}
	for _, item := range first.Items {
		if item.Resource.Application != app || item.Resource.Version != "1.0.0" {
			t.Fatal("resource filter crossed scope")
		}
	}
	last := decodeList[download.View](t, listRequest(s, app, "resources", query+"&cursor="+*first.NextCursor))
	if len(last.Items) != 1 || last.NextCursor != nil || last.Items[0].ID <= first.Items[1].ID || last.Items[0].Resource.Application != app || last.Items[0].Resource.Version != "1.0.0" {
		t.Fatal("resource continuation skipped filter or repeated row")
	}
	for _, bad := range []struct{ app, endpoint, query string }{{app, "resources", "?version=1.0.1&cursor=" + *first.NextCursor}, {other, "resources", query + "&cursor=" + *first.NextCursor}, {app, "versions", "?cursor=" + *first.NextCursor}, {app, "resources", "?version=v1.0.0"}, {app, "resources", "?version=1.0.0&version=1.0.1"}} {
		if w := listRequest(s, bad.app, bad.endpoint, bad.query); w.Code != 400 {
			t.Fatal("invalid filter/cursor accepted", bad, w.Code)
		}
	}
	empty := decodeList[download.View](t, listRequest(s, app, "resources", "?version=9.0.0"))
	if len(empty.Items) != 0 || empty.NextCursor != nil {
		t.Fatal("empty filtered page is not final")
	}
}
