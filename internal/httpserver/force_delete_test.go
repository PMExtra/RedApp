package httpserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func deleteBody(a store.Application) map[string]any {
	return map[string]any{"revision": a.Revision, "confirm_key": a.Key, "confirm_uid": a.UID}
}
func awaitClosed(t *testing.T, c <-chan struct{}) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(3 * time.Second):
		t.Fatal("task did not exit")
	}
}

func TestForceDeleteCancelsCacheOriginWithoutInterruptingSibling(t *testing.T) {
	startedA, startedB, stoppedA, finishB := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=3600")
		w.Write([]byte("start"))
		w.(http.Flusher).Flush()
		if strings.HasPrefix(r.URL.Path, "/a/") {
			close(startedA)
			<-r.Context().Done()
			close(stoppedA)
			return
		}
		close(startedB)
		select {
		case <-finishB:
			io.WriteString(w, "finish")
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	h := newForceHarness(t, t.TempDir())
	h.login(h.password)
	h.createVendor("force")
	a := h.createApp("force", "a", "http-cache", map[string]any{"base_url": upstream.URL + "/a"})
	b := h.createApp("force", "b", "http-cache", map[string]any{"base_url": upstream.URL + "/b"})
	type result struct {
		body string
		err  error
	}
	read := func(path string) <-chan result {
		ch := make(chan result, 1)
		go func() {
			r, e := http.Get(h.http.URL + path)
			if e != nil {
				ch <- result{err: e}
				return
			}
			defer r.Body.Close()
			data, e := io.ReadAll(r.Body)
			ch <- result{string(data), e}
		}()
		return ch
	}
	ar, br := read("/force/a/file"), read("/force/b/file")
	awaitClosed(t, startedA)
	awaitClosed(t, startedB)
	// Invalid requests must not cancel anything.
	bad := deleteBody(a)
	bad["revision"] = a.Revision + 100
	h.request("DELETE", "/admin/api/apps/"+a.Key, bad, 409, nil)
	select {
	case <-stoppedA:
		t.Fatal("revision rejection canceled transfer")
	default:
	}
	h.request("DELETE", "/admin/api/apps/"+a.Key, deleteBody(a), 200, nil)
	awaitClosed(t, stoppedA)
	select {
	case got := <-ar:
		if got.err == nil && got.body == "startfinish" {
			t.Fatal("target unexpectedly completed")
		}
	case <-time.After(time.Second):
		t.Fatal("target response survived deletion")
	}
	select {
	case <-br:
		t.Fatal("sibling transfer was interrupted")
	default:
	}
	close(finishB)
	select {
	case got := <-br:
		if got.err != nil || got.body != "startfinish" {
			t.Fatal(got)
		}
	case <-time.After(time.Second):
		t.Fatal("sibling did not finish")
	}
	if _, err := h.server.DB.Application(b.Key); err != nil {
		t.Fatal(err)
	}
	h.request("DELETE", "/admin/api/apps/"+a.Key, deleteBody(a), 200, nil)
	replacement := h.createApp("force", "a", "info", nil)
	h.request("DELETE", "/admin/api/apps/"+a.Key, deleteBody(a), 409, nil)
	if row, err := h.server.DB.Application(replacement.Key); err != nil || row.UID != replacement.UID || row.DeletedAt != nil {
		t.Fatal("old delete reached replacement", row, err)
	}
}

func TestForceDeleteTimeoutBlocksAdmissionAndRestartsFromIntent(t *testing.T) {
	dir := t.TempDir()
	h := newForceHarness(t, dir, func(s *Server) { s.deleteWait = 20 * time.Millisecond })
	h.login(h.password)
	h.createVendor("force")
	a := h.createApp("force", "held", "hosted", nil)
	entry, _ := h.server.Registry.Lookup(a.Key)
	file, err := h.server.Hosted.Put(context.Background(), entry, "file", "", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", func(context.Context) (io.ReadCloser, int64, error) {
		return io.NopCloser(strings.NewReader("keep until drained")), 18, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, release, err := h.server.DB.ApplicationWork(context.Background(), a.StorageID())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	data, _ := h.request("DELETE", "/admin/api/apps/"+a.Key, deleteBody(a), 409, nil)
	if !strings.Contains(string(data), "DIRECTORY_DELETE_PENDING") || !strings.Contains(string(data), `"retryable":true`) {
		t.Fatal(string(data))
	}
	if ctx.Err() == nil {
		t.Fatal("target was not canceled")
	}
	if _, err = os.Stat(filepath.Join(dir, "objects", "hosted", file.ID)); err != nil {
		t.Fatal("files removed before exit", err)
	}
	if _, _, err = h.server.DB.ApplicationWork(context.Background(), a.StorageID()); !errors.Is(err, store.ErrSourceInactive) {
		t.Fatal("new work admitted", err)
	}
	h.request("DELETE", "/admin/api/apps/"+a.Key, deleteBody(a), 409, nil)
	// A failed final transaction must retain the intent and files, with the same retry response.
	release()
	if _, err = h.server.DB.DB.Exec(`CREATE TEMP TRIGGER fail_final_delete BEFORE DELETE ON applications BEGIN SELECT RAISE(ABORT,'isolated final-delete failure'); END`); err != nil {
		t.Fatal(err)
	}
	h.request("DELETE", "/admin/api/apps/"+a.Key, deleteBody(a), 409, nil)
	if _, err = os.Stat(filepath.Join(dir, "objects", "hosted", file.ID)); err != nil {
		t.Fatal("failed transaction deleted the body", err)
	}
	// Startup recovery handles only explicit permanent-deletion intents.
	soft := h.createApp("force", "soft", "info", nil)
	if err = h.server.DB.DeleteApplication(soft.Key, soft.Revision); err != nil {
		t.Fatal(err)
	}
	release()
	h.close()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.DB.Close()
	if _, _, err = db.ApplicationWork(context.Background(), a.UID); !errors.Is(err, store.ErrSourceInactive) {
		t.Fatal("restart admitted pending UID", err)
	}
	if err = db.RecoverApplicationDeletions(); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Application(a.Key); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("intent not recovered", err)
	}
	if _, err = db.Application(soft.Key); err != nil {
		t.Fatal("unrelated soft delete was purged", err)
	}
	if err = db.ProcessPendingDeletes(dir); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, "objects", "hosted", file.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("body survived recovery", err)
	}
	if err = db.RecoverApplicationDeletions(); err != nil {
		t.Fatal("non-idempotent recovery", err)
	}
}

func TestForceDeleteStopsHostedImportBeforePublishing(t *testing.T) {
	h := newForceHarness(t, t.TempDir())
	h.login(h.password)
	h.createVendor("force")
	a := h.createApp("force", "import", "hosted", nil)
	entry, _ := h.server.Registry.Lookup(a.Key)
	reader, writer := io.Pipe()
	defer writer.Close()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := h.server.Hosted.Put(context.Background(), entry, "file", "", "cccccccccccccccccccccccccccccccc", func(context.Context) (io.ReadCloser, int64, error) { close(started); return reader, -1, nil })
		done <- err
	}()
	awaitClosed(t, started)
	h.request("DELETE", "/admin/api/apps/"+a.Key, deleteBody(a), 200, nil)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("import committed after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("import did not exit")
	}
	if _, err := h.server.DB.HostedFile(a.UID, "file"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	files, err := os.ReadDir(filepath.Join(h.server.Dir, "objects", "hosted"))
	if err != nil || len(files) != 0 {
		t.Fatal("orphan import body", files, err)
	}
	opened := false
	_, err = h.server.Hosted.Put(context.Background(), entry, "file", "", "dddddddddddddddddddddddddddddddd", func(context.Context) (io.ReadCloser, int64, error) {
		opened = true
		return io.NopCloser(strings.NewReader("late")), 4, nil
	})
	if err == nil || opened {
		t.Fatal("stale snapshot restarted import", err)
	}
}

func TestForceDeleteAbortsHostedHTTP2StreamAndPreservesSiblingStream(t *testing.T) {
	startedB, finishB := make(chan struct{}), make(chan struct{})
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(startedB)
		select {
		case <-finishB:
			io.WriteString(w, "sibling remains alive")
		case <-r.Context().Done():
		}
	}))
	defer origin.Close()
	h := newForceHarness(t, t.TempDir())
	h.login(h.password)
	h.createVendor("force")
	a := h.createApp("force", "hosted", "hosted", nil)
	h.createApp("force", "sibling", "http-cache", map[string]any{"base_url": origin.URL})
	entry, _ := h.server.Registry.Lookup(a.Key)
	const size = 16 << 20
	_, err := h.server.Hosted.Put(context.Background(), entry, "large", "", "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", func(context.Context) (io.ReadCloser, int64, error) {
		return io.NopCloser(strings.NewReader(strings.Repeat("x", size))), size, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(h.server)
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	client := server.Client()
	client.Timeout = 5 * time.Second
	response, err := client.Get(server.URL + "/force/hosted/large")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.ProtoMajor != 2 {
		t.Fatal("fixture must use HTTP/2")
	}
	doneB := make(chan error, 1)
	go func() {
		r, e := client.Get(server.URL + "/force/sibling/file")
		if e == nil {
			defer r.Body.Close()
			var body []byte
			body, e = io.ReadAll(r.Body)
			if e == nil && string(body) != "sibling remains alive" {
				e = errors.New("wrong sibling body")
			}
		}
		doneB <- e
	}()
	awaitClosed(t, startedB)
	h.request("DELETE", "/admin/api/apps/"+a.Key, deleteBody(a), 200, nil)
	data, err := io.ReadAll(response.Body)
	if err == nil || len(data) == size {
		t.Fatal("held target stream was not aborted", len(data), err)
	}
	select {
	case err := <-doneB:
		t.Fatal("sibling stream ended during target deletion", err)
	default:
	}
	close(finishB)
	select {
	case err := <-doneB:
		if err != nil {
			t.Fatal("sibling stream interrupted", err)
		}
	case <-time.After(time.Second):
		t.Fatal("sibling stream did not finish")
	}
}

// These are deletion tests, not password-work-factor benchmarks. Production and
// the authentication regression suite continue to use the real bcrypt cost.
func newForceHarness(t *testing.T, dir string, configure ...func(*Server)) *directoryHarness {
	t.Helper()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	const password = "isolated-deletion-test-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec(`INSERT INTO admin VALUES(1,?,1)`, hash); err != nil {
		t.Fatal(err)
	}
	h := newDirectoryHarnessWithStore(t, dir, db, configure...)
	h.password = password
	return h
}

// Metadata and installer writes can block too, even without an active origin.
type heldApplicationResponse struct {
	headers          http.Header
	started, stopped chan struct{}
	start, stop      sync.Once
}

func (w *heldApplicationResponse) Header() http.Header { return w.headers }
func (w *heldApplicationResponse) WriteHeader(int)     {}
func (w *heldApplicationResponse) Write(p []byte) (int, error) {
	w.start.Do(func() { close(w.started) })
	<-w.stopped
	return 0, context.Canceled
}
func (w *heldApplicationResponse) SetWriteDeadline(time.Time) error {
	w.stop.Do(func() { close(w.stopped) })
	return nil
}
func (w *heldApplicationResponse) SetReadDeadline(time.Time) error { return nil }
func TestForceDeleteDrainsCachedMetadataResponse(t *testing.T) {
	var calls atomic.Int64
	var base string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprintf(w, `{"tag_name":"rust-v1.0.0","assets":[{"name":"asset.tgz","digest":"sha256:%s","browser_download_url":"%s/releases/1.0.0/asset.tgz"}]}`, strings.Repeat("a", 64), base)
	}))
	defer origin.Close()
	base = origin.URL
	h := newForceHarness(t, t.TempDir())
	h.login(h.password)
	h.createVendor("force")
	a := h.createApp("force", "release", "codex", map[string]any{"base_url": base})
	path := "/force/release/releases/1.0.0/release.json"
	h.request("GET", path, nil, 200, nil)
	held := &heldApplicationResponse{headers: make(http.Header), started: make(chan struct{}), stopped: make(chan struct{})}
	defer held.SetWriteDeadline(time.Now())
	done := make(chan struct{})
	go func() { defer close(done); h.server.ServeHTTP(held, httptest.NewRequest("GET", h.http.URL+path, nil)) }()
	awaitClosed(t, held.started)
	if calls.Load() != 1 {
		t.Fatal("fixture must serve durable metadata cache")
	}
	h.request("DELETE", "/admin/api/apps/"+a.Key, deleteBody(a), 200, nil)
	awaitClosed(t, done)
}
