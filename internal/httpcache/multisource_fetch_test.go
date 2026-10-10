package httpcache

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/store"
)

func additionalSource(t *testing.T, f *fixture, strategy string, h http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	bases := []string{f.entry.Upstream.Base.String(), server.URL + "/files"}
	updated, err := f.db.UpdateApplication(f.app.Key, f.entry.Revision, store.ApplicationChanges{Name: f.app.Name, BaseURLs: bases, SourceStrategy: strategy, CacheTTLSeconds: f.entry.Descriptor.DefaultChannelTTLSeconds, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	f.app = updated
	f.entry.Revision = updated.Revision
	f.entry.RuntimeRevision = updated.RuntimeRevision
	f.entry.SourceEpoch = updated.SourceEpoch
	f.entry.SourceStrategy = strategy
	client, err := distributor.NewPool().NewClient(bases[1], distributor.GeneralHTTP)
	if err != nil {
		t.Fatal(err)
	}
	f.entry.Upstreams = []*distributor.Client{f.entry.Upstream, client}
	return server
}

func TestMultipleSourceFailuresRestartWholeBodyAndTerminalStatus(t *testing.T) {
	for _, mode := range []string{"partial", "connection", "server-error", "not-found", "gone", "forbidden", "unsafe-encoding", "direct-partial"} {
		t.Run(mode, func(t *testing.T) {
			var nextCalls atomic.Int64
			f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "partial", "direct-partial":
					if mode == "direct-partial" {
						w.Header().Set("Cache-Control", "no-store")
					}
					w.Header().Set("Content-Length", "20")
					io.WriteString(w, "PARTIAL")
				case "connection":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					conn.Close()
				case "server-error":
					w.WriteHeader(503)
				case "not-found":
					w.WriteHeader(404)
				case "gone":
					w.WriteHeader(410)
				case "forbidden":
					w.WriteHeader(403)
				case "unsafe-encoding":
					w.Header().Set("Content-Encoding", "gzip")
					io.WriteString(w, "invalid")
				}
			}), 300)
			additionalSource(t, f, "ordered", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalls.Add(1)
				if r.Header.Get("Range") != "" || r.Header.Get("If-None-Match") != "" {
					t.Error("partial fetch carried state across sources")
				}
				io.WriteString(w, "SECOND-COMPLETE")
			}))
			w := httptest.NewRecorder()
			var err error
			func() {
				defer func() {
					if caught := recover(); caught != nil {
						if caught != http.ErrAbortHandler {
							panic(caught)
						}
						err = http.ErrAbortHandler
					}
				}()
				err = f.s.Serve(w, httptest.NewRequest("GET", "http://redapp/file", nil), f.entry, "file")
			}()
			switch mode {
			case "partial", "connection", "server-error":
				if err != nil || w.Body.String() != "SECOND-COMPLETE" || nextCalls.Load() != 1 {
					t.Fatal(w.Body.String(), err, nextCalls.Load())
				}
				rows := f.rows(t)
				if len(rows) != 1 || rows[0].SourceURL != f.entry.Upstreams[1].Base.String()+"/file" {
					t.Fatal(rows)
				}
				body, e := os.ReadFile(f.s.bodyPath(rows[0].GenerationID))
				if e != nil || string(body) != "SECOND-COMPLETE" {
					t.Fatal(string(body), e)
				}
			case "not-found", "gone", "forbidden":
				want := map[string]int{"not-found": 404, "gone": 410, "forbidden": 403}[mode]
				if err != nil || w.Code != want || nextCalls.Load() != 0 || len(f.rows(t)) != 0 {
					t.Fatal(w.Code, err, nextCalls.Load())
				}
			case "unsafe-encoding", "direct-partial":
				if err == nil || nextCalls.Load() != 0 || len(f.rows(t)) != 0 {
					t.Fatal("unsafe/direct response retried", err, nextCalls.Load())
				}
				if mode == "direct-partial" && w.Body.String() != "PARTIAL" {
					t.Fatal(w.Body.String())
				}
			}
			files, e := os.ReadDir(f.s.dir)
			if e != nil {
				t.Fatal(e)
			}
			for _, file := range files {
				if strings.HasSuffix(file.Name(), ".tmp") {
					t.Fatal("incomplete source spool survived", file.Name())
				}
			}
		})
	}
}

func TestSourceValidatorsRemainBoundAndAllFailuresFallbackOnce(t *testing.T) {
	var firstFail, allFail atomic.Bool
	var firstCalls, secondCalls atomic.Int64
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls.Add(1)
		if firstFail.Load() {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("ETag", `"same-tag"`)
		io.WriteString(w, "AAA")
	}), 0)
	additionalSource(t, f, "ordered", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls.Add(1)
		if allFail.Load() {
			w.WriteHeader(503)
			return
		}
		if secondCalls.Load() == 1 && (r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Modified-Since") != "") {
			t.Error("first mirror reused another source validator")
		}
		w.Header().Set("ETag", `"same-tag"`)
		if r.Header.Get("If-None-Match") == `"same-tag"` {
			w.WriteHeader(304)
			return
		}
		io.WriteString(w, "BBB")
	}))
	if w, e := f.serve(t, "GET", http.Header{}); e != nil || w.Body.String() != "AAA" {
		t.Fatal(w, e)
	}
	firstFail.Store(true)
	if w, e := f.serve(t, "GET", http.Header{}); e != nil || w.Body.String() != "BBB" {
		t.Fatal(w, e)
	}
	row := f.rows(t)[0]
	f.clock.Add(1)
	if w, e := f.serve(t, "GET", http.Header{}); e != nil || w.Body.String() != "BBB" {
		t.Fatal(w, e)
	}
	validated := f.rows(t)[0]
	if validated.GenerationID != row.GenerationID || !validated.ValidatedAt.After(row.ValidatedAt) {
		t.Fatal("same-source 304 lost validator optimization", row, validated)
	}
	allFail.Store(true)
	if w, e := f.serve(t, "GET", http.Header{}); e != nil || w.Body.String() != "BBB" {
		t.Fatal(w, e)
	}
	if len(warningEvents(t, f, "stale_cache_fallback")) != 1 {
		t.Fatal("fallback warning emitted per failed source")
	}
	config := explicitPolicy(0)
	config.StaleFallback = false
	setPolicy(t, f, config)
	if _, e := f.serve(t, "GET", http.Header{}); !errors.Is(e, ErrUpstream) {
		t.Fatal("all failed sources bypassed stale switch", e)
	}
	if len(warningEvents(t, f, "stale_cache_fallback")) != 1 || f.rows(t)[0].GenerationID != row.GenerationID {
		t.Fatal("disabled fallback modified stored body")
	}
	if firstCalls.Load() != 5 || secondCalls.Load() != 4 {
		t.Fatal(firstCalls.Load(), secondCalls.Load())
	}
}

func TestHeadDoesNotRevalidateAnotherSourcesEqualETag(t *testing.T) {
	var fail atomic.Bool
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("ETag", `"same"`)
		io.WriteString(w, "AAA")
	}), 0)
	additionalSource(t, f, "ordered", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Modified-Since") != "" {
			t.Error("HEAD/GET carried validator to a different source")
		}
		w.Header().Set("ETag", `"same"`)
		w.Header().Set("Content-Length", "3")
		w.Header().Set("Cache-Control", "max-age=600")
		if r.Method != "HEAD" {
			io.WriteString(w, "BBB")
		}
	}))
	if _, e := f.serve(t, "GET", http.Header{}); e != nil {
		t.Fatal(e)
	}
	before := f.rows(t)[0]
	fail.Store(true)
	f.clock.Add(60)
	if w, e := f.serve(t, "HEAD", http.Header{}); e != nil || w.Code != 200 || w.Body.Len() != 0 {
		t.Fatal(w, e)
	}
	after := f.rows(t)[0]
	if after.GenerationID != before.GenerationID || !after.ValidatedAt.Equal(before.ValidatedAt) || after.SourceURL != before.SourceURL {
		t.Fatal("different-source HEAD extended old body", before, after)
	}
	if w, e := f.serve(t, "GET", http.Header{}); e != nil || w.Body.String() != "BBB" {
		t.Fatal(w, e)
	}
}

func TestRoundRobinAdvancesOnlyForRealUpstreamFetch(t *testing.T) {
	var first, second atomic.Int64
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { first.Add(1); io.WriteString(w, "AAA") }), 300)
	additionalSource(t, f, "round_robin", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { second.Add(1); io.WriteString(w, "BBB") }))
	for i := 0; i < 3; i++ {
		if w, e := f.serve(t, "GET", http.Header{}); e != nil || w.Body.String() != "AAA" {
			t.Fatal(w, e)
		}
	}
	if first.Load() != 1 || second.Load() != 0 {
		t.Fatal(first.Load(), second.Load())
	}
	f.clock.Add(400)
	if w, e := f.serve(t, "GET", http.Header{}); e != nil || w.Body.String() != "BBB" {
		t.Fatal(w, e)
	}
	if first.Load() != 1 || second.Load() != 1 {
		t.Fatal("cache hit advanced source rotation", first.Load(), second.Load())
	}
}

func TestRedirectResponseIdentityPreventsValidatorReuse(t *testing.T) {
	var phase atomic.Int64
	var targetCalls atomic.Int64
	f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"shared-looking-tag"`)
		if phase.Load() == 0 {
			io.WriteString(w, "original")
			return
		}
		if r.URL.Path == "/files/file" {
			if phase.Load() > 1 && (r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Modified-Since") != "") {
				t.Error("redirected representation validator sent to initial source")
			}
			http.Redirect(w, r, "/files/target", http.StatusTemporaryRedirect)
			return
		}
		targetCalls.Add(1)
		if r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Modified-Since") != "" {
			t.Error("validator followed redirect")
		}
		if phase.Load() == 2 {
			w.WriteHeader(304)
			return
		}
		io.WriteString(w, "redirected")
	}), 0)
	if _, err := f.serve(t, "GET", http.Header{}); err != nil {
		t.Fatal(err)
	}
	initial := f.rows(t)[0]
	phase.Store(1)
	if w, err := f.serve(t, "GET", http.Header{}); err != nil || w.Body.String() != "redirected" {
		t.Fatal(w, err)
	}
	redirected := f.rows(t)[0]
	if redirected.SourceURL != f.entry.Upstream.Base.String()+"/target" || initial.SourceURL != f.entry.Upstream.Base.String()+"/file" || redirected.GenerationID == initial.GenerationID {
		t.Fatal(initial, redirected)
	}
	phase.Store(2)
	for _, method := range []string{"GET", "HEAD"} {
		if _, err := f.serve(t, method, http.Header{}); !errors.Is(err, ErrUpstream) {
			t.Fatal("unsolicited redirect 304 reused a body", method, err)
		}
	}
	if f.rows(t)[0].GenerationID != redirected.GenerationID || targetCalls.Load() != 3 {
		t.Fatal("invalid redirect validation mutated cache")
	}
}

func TestRedirectLoopBackCannotValidateOriginalBody(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		t.Run(method, func(t *testing.T) {
			var redirect atomic.Bool
			var initialCalls atomic.Int64
			f := newFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("ETag", `"same"`)
				if !redirect.Load() {
					io.WriteString(w, "original")
					return
				}
				if r.URL.Path == "/files/hop" {
					http.Redirect(w, r, "/files/file", 307)
					return
				}
				if initialCalls.Add(1) == 1 {
					http.Redirect(w, r, "/files/hop", 307)
					return
				}
				if r.Header.Get("If-None-Match") != "" {
					t.Error("loop restored conditional header")
				}
				w.WriteHeader(304)
			}), 0)
			if _, err := f.serve(t, "GET", http.Header{}); err != nil {
				t.Fatal(err)
			}
			before := f.rows(t)[0]
			redirect.Store(true)
			f.clock.Add(60)
			if _, err := f.serve(t, method, http.Header{}); !errors.Is(err, ErrUpstream) {
				t.Fatal("same-URL redirect loop accepted an unsolicited 304", err)
			}
			if !f.rows(t)[0].ValidatedAt.Equal(before.ValidatedAt) {
				t.Fatal("redirect loop refreshed cached body")
			}
		})
	}
}
