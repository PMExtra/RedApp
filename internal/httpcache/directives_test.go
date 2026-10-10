package httpcache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/cachepolicy"
	"github.com/PMExtra/RedApp/internal/pathmatch"
)

func warningEvents(t *testing.T, f *fixture, code string) []map[string]any {
	t.Helper()
	events, err := f.db.EventsFor(f.entry.MetricsID())
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, event := range events {
		if event["code"] == code {
			out = append(out, event)
		}
	}
	return out
}
func explicitPolicy(ttl int) cachepolicy.Config {
	c := cachepolicy.Empty()
	c.Rules = []cachepolicy.CacheRule{{ID: "file-rule", Match: pathmatch.Spec{Type: "glob", Pattern: "/file"}, TTLSeconds: ttl}}
	return c
}

func TestExplicitDirectiveOverrideAndValidationWarnings(t *testing.T) {
	var calls atomic.Int64
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Cache-Control", `no-store, private="secret-field", no-cache, max-age=0`)
		w.Header().Set("Age", "9000")
		w.Header().Set("ETag", `"v1"`)
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(304)
			return
		}
		io.WriteString(w, "complete")
	}), 1)
	setPolicy(t, f, explicitPolicy(60))
	for i := 0; i < 2; i++ {
		w, e := f.serve(t, "GET", http.Header{})
		if e != nil || w.Body.String() != "complete" {
			t.Fatal(w, e)
		}
	}
	rows := f.rows(t)
	if len(rows) != 1 || !rows[0].FreshUntil.Equal(rows[0].ValidatedAt.Add(time.Minute)) || calls.Load() != 1 {
		t.Fatal(rows, calls.Load())
	}
	if n := len(warningEvents(t, f, "cache_rule_override")); n != 1 {
		t.Fatal("cache hit duplicated warning", n)
	}
	f.clock.Add(61)
	if _, e := f.serve(t, "GET", http.Header{}); e != nil {
		t.Fatal(e)
	}
	events := warningEvents(t, f, "cache_rule_override")
	if len(events) != 2 || calls.Load() != 2 {
		t.Fatal("304 override not recorded once", events, calls.Load())
	}
	for _, event := range events {
		message := fmt.Sprint(event["message"])
		if event["category"] != "warning" || event["resource_key"] != "file" || !strings.Contains(message, "file-rule") || !strings.Contains(message, "no-store, private") || strings.Contains(message, "secret-field") || strings.Contains(message, "complete") {
			t.Fatal("unsafe or incomplete warning", event)
		}
	}
}

func TestTTLZeroStaleSwitchRetainsBodyAndSuppressesOverride(t *testing.T) {
	var calls atomic.Int64
	var failed atomic.Bool
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if failed.Load() {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Cache-Control", "no-store, private")
		io.WriteString(w, "complete")
	}), 300)
	config := explicitPolicy(0)
	setPolicy(t, f, config)
	for i := 0; i < 2; i++ {
		if _, e := f.serve(t, "GET", http.Header{}); e != nil {
			t.Fatal(e)
		}
	}
	if calls.Load() != 2 || len(f.rows(t)) != 1 || len(warningEvents(t, f, "cache_rule_override")) != 0 {
		t.Fatal("TTL zero did not retain/revalidate without warning")
	}
	before := f.rows(t)[0]
	failed.Store(true)
	w, e := f.serve(t, "GET", http.Header{})
	if e != nil || w.Body.String() != "complete" || len(warningEvents(t, f, "stale_cache_fallback")) != 1 {
		t.Fatal(w, e)
	}
	config.StaleFallback = false
	epoch := f.entry.SourceEpoch
	setPolicy(t, f, config)
	if _, e = f.serve(t, "GET", http.Header{}); !errors.Is(e, ErrUpstream) {
		t.Fatal("disabled fallback served stale", e)
	}
	after := f.rows(t)[0]
	if after.GenerationID != before.GenerationID || !after.ValidatedAt.Equal(before.ValidatedAt) || f.entry.SourceEpoch != epoch || len(warningEvents(t, f, "stale_cache_fallback")) != 1 {
		t.Fatal("switch changed body/epoch/timestamps or warned without fallback")
	}
	config.StaleFallback = true
	setPolicy(t, f, config)
	if _, e = f.serve(t, "GET", http.Header{}); e != nil {
		t.Fatal(e)
	}
	if len(warningEvents(t, f, "stale_cache_fallback")) != 2 || len(warningEvents(t, f, "cache_rule_override")) != 0 {
		t.Fatal("warnings disagree with actual fallback")
	}
	// Removing explicit permission preserves the physical body but disallows
	// serving a private/no-store representation through default fallback.
	setPolicy(t, f, cachepolicy.Empty())
	if _, e = f.serve(t, "GET", http.Header{}); !errors.Is(e, ErrUpstream) {
		t.Fatal("removed rule retained private override", e)
	}
	if len(f.rows(t)) != 1 {
		t.Fatal("policy update deleted cached body")
	}
}

func TestOverrideDoesNotRelaxRepresentationIsolation(t *testing.T) {
	for _, tc := range []struct {
		name, header, value, reason string
		explicit                    bool
	}{
		{"cookie", "Set-Cookie", "token=secret", "set-cookie", true},
		{"vary-auth", "Vary", "Authorization", "unsupported-vary", true},
		{"vary-star", "Vary", "*", "unsupported-vary", true},
		{"default-no-store", "Cache-Control", "no-store", "source-no-store", false},
		{"default-private", "Cache-Control", "private", "source-private", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set(tc.header, tc.value)
				io.WriteString(w, "body")
			}), 300)
			if tc.explicit {
				setPolicy(t, f, explicitPolicy(60))
			}
			for i := 0; i < 2; i++ {
				w, e := f.serve(t, "GET", http.Header{})
				if e != nil || w.Body.String() != "body" {
					t.Fatal(w, e)
				}
			}
			if calls.Load() != 2 || len(f.rows(t)) != 0 || len(warningEvents(t, f, "cache_rule_override")) != 0 {
				t.Fatal("isolation boundary was relaxed")
			}
			if got := cacheBlockReason(http.Header{tc.header: {tc.value}}, CacheDecision{Explicit: tc.explicit}); got != tc.reason {
				t.Fatal(got)
			}
		})
	}
}

func TestSourceTTLPrecedesDefaultAndNoControlUsesLocalDefault(t *testing.T) {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		header http.Header
		want   time.Duration
	}{
		{"source-over-default", http.Header{"Cache-Control": {"max-age=600"}}, 600 * time.Second},
		{"source-over-zero-default", http.Header{"Cache-Control": {"s-maxage=600,max-age=30,no-cache,must-revalidate"}, "Age": {"10"}}, 590 * time.Second},
		{"age", http.Header{"Cache-Control": {"max-age=600"}, "Date": {now.Add(-40 * time.Second).Format(http.TimeFormat)}, "Age": {"10"}}, 560 * time.Second},
		{"no-control", http.Header{"Expires": {"0"}, "Date": {now.Add(-time.Hour).Format(http.TimeFormat)}, "Age": {"9999"}}, 60 * time.Second},
		{"quoted-directive-argument", http.Header{"Cache-Control": {`no-cache="fields,max-age=600"`}}, 0},
		{"no-lifetime", http.Header{"Cache-Control": {"public"}, "Expires": {now.Add(time.Hour).Format(http.TimeFormat)}}, 0},
		{"empty-control", http.Header{"Cache-Control": {""}}, 0},
		{"invalid-shared", http.Header{"Cache-Control": {"s-maxage=bad,max-age=600"}}, 0},
		{"duplicates", http.Header{"Cache-Control": {"max-age=600,max-age=600"}}, 0},
		{"overflow-lifetime", http.Header{"Cache-Control": {"max-age=9223372036854775807"}}, 0},
		{"source-zero", http.Header{"Cache-Control": {"max-age=0"}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ttl := 60
			if tc.name == "source-over-zero-default" {
				ttl = 0
			}
			if got := freshness(tc.header, ttl, now).Sub(now); got != tc.want {
				t.Fatal(got, tc.want)
			}
		})
	}
}

// joinedContext exposes the wait boundary without timing-based sleeps: a
// follower asks Done only after selecting the already-running shared flight.
type joinedContext struct {
	context.Context
	joined chan struct{}
	once   sync.Once
}

func (c *joinedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.joined) })
	return c.Context.Done()
}

func TestSharedUpstreamEmitsOneWarningForAllReaders(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(fmt.Sprint("stale=", stale), func(t *testing.T) {
			var blocking atomic.Bool
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if blocking.Load() {
					once.Do(func() { close(entered) })
					<-release
					if stale {
						w.WriteHeader(503)
						return
					}
				}
				w.Header().Set("Cache-Control", "no-store, private")
				io.WriteString(w, "body")
			}), 0)
			ttl := 60
			if stale {
				ttl = 0
			}
			setPolicy(t, f, explicitPolicy(ttl))
			if stale {
				if _, err := f.serve(t, "GET", http.Header{}); err != nil {
					t.Fatal(err)
				}
			}
			policy, err := f.s.readPolicy(f.entry)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			fileFill := fill{entry: f.entry, path: "file", policy: policy}
			old, err := f.s.lookup(ctx, f.entry.StorageID(), "file")
			if err != nil {
				t.Fatal(err)
			}
			if old != nil {
				defer f.s.unpin(old.GenerationID)
			}
			blocking.Store(true)
			done := make(chan error, 4)
			run := func(ctx context.Context) {
				result, err := f.s.sharedFetch(ctx, fileFill, old)
				if err == nil {
					_, err = f.consume(result)
				}
				done <- err
			}
			go run(ctx)
			<-entered
			for i := 0; i < 3; i++ {
				waitCtx := &joinedContext{Context: ctx, joined: make(chan struct{})}
				go run(waitCtx)
				<-waitCtx.joined
			}
			close(release)
			for i := 0; i < 4; i++ {
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			}
			code := "cache_rule_override"
			if stale {
				code = "stale_cache_fallback"
			}
			if count := len(warningEvents(t, f, code)); count != 1 {
				t.Fatal("per-reader warning duplicated shared upstream event", code, count)
			}
		})
	}
}

func TestPolicyRevisionSeparatesFlightsAndFencesOldPublication(t *testing.T) {
	for _, returnOldBody := range []bool{false, true} {
		t.Run(fmt.Sprint("failure=", returnOldBody), func(t *testing.T) {
			var calls atomic.Int64
			entered, release := make(chan struct{}), make(chan struct{})
			f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				if n == 2 {
					close(entered)
					<-release
					if returnOldBody {
						w.WriteHeader(503)
						return
					}
					io.WriteString(w, "old-policy-new-body")
					return
				}
				if n > 2 {
					w.WriteHeader(503)
					return
				}
				io.WriteString(w, "original")
			}), 0)
			if _, err := f.serve(t, "GET", http.Header{}); err != nil {
				t.Fatal(err)
			}
			before := f.rows(t)[0]
			oldEntry := f.entry
			done := make(chan error, 1)
			go func() {
				done <- f.s.Serve(httptest.NewRecorder(), httptest.NewRequest("GET", "http://redapp/file", nil), oldEntry, "file")
			}()
			<-entered
			config := cachepolicy.Empty()
			config.StaleFallback = false
			setPolicy(t, f, config)
			if _, err := f.serve(t, "GET", http.Header{}); !errors.Is(err, ErrUpstream) {
				t.Fatal("new policy joined old fallback flight", err)
			}
			close(release)
			// The admitted request keeps its snapshot: it falls back to the old
			// body, or receives the new body, which is never published under
			// the changed policy.
			if err := <-done; err != nil {
				t.Fatal("admitted snapshot lost its response", err)
			}
			if f.rows(t)[0].GenerationID != before.GenerationID {
				t.Fatal("policy update changed stored body")
			}
		})
	}
}
