package download

import (
	"bytes"
	"context"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/testutil"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
)

func TestTwoApplicationsShareLimitsAndKeepCleanupSeparate(t *testing.T) {
	payload := bytes.Repeat([]byte("same version and asset, separate origins"), 1000)
	var counts [2]atomic.Int32
	clients := map[string]*distributor.Client{}
	for i, id := range []string{"codex", "claude-code"} {
		idx := i
		client, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { counts[idx].Add(1); w.Write(payload) }))
		clients[id] = client
	}
	dir := t.TempDir()
	db, e := store.Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.DB.Close()
	m, e := NewApplications(dir, db, clients)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	resources := map[string]Resource{}
	for id, c := range clients {
		r := resource(c, payload)
		r.Labels["app"] = id
		resources[id] = r
	}
	var wg sync.WaitGroup
	for id := range clients {
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				r, _, e := m.Acquire(context.Background(), resources[id])
				if e != nil {
					t.Error(e)
					return
				}
				defer r.Close()
				b, e := io.ReadAll(r)
				if e != nil || !bytes.Equal(b, payload) {
					t.Errorf("%s payload: %v", id, e)
				}
			}(id)
		}
	}
	wg.Wait()
	if counts[0].Load() != 1 || counts[1].Load() != 1 {
		t.Fatal("per-application shared download failed")
	}
	if resources["codex"].ID == resources["claude-code"].ID {
		t.Fatal("identities collided")
	}
	wrong := resources["claude-code"]
	wrong.Labels = map[string]string{"app": "codex"}
	if r, _, e := m.Acquire(context.Background(), wrong); e == nil {
		r.Close()
		t.Fatal("foreign upstream was served from cache")
	}
	wrong.Labels = map[string]string{"app": "unknown"}
	if r, _, e := m.Acquire(context.Background(), wrong); e == nil {
		r.Close()
		t.Fatal("unknown app")
	}
	old, _, e := m.Acquire(context.Background(), resources["codex"])
	if e != nil {
		t.Fatal(e)
	}
	defer old.Close()
	job, e := m.Preview(map[string]bool{resources["codex"].ID: true})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Cleanup(job.ID); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(collect(t, m, resources["claude-code"]), payload) || counts[1].Load() != 1 {
		t.Fatal("cleanup affected other app")
	}
	if !bytes.Equal(collect(t, m, resources["codex"]), payload) || counts[0].Load() != 2 {
		t.Fatal("new generation not independent")
	}
	if b, e := io.ReadAll(old); e != nil || !bytes.Equal(b, payload) {
		t.Fatal("old reader failed to drain", e)
	}
	// Global reader limit, not a separate quota for each application.
	m.mu.Lock()
	m.maxReaders = 1
	m.mu.Unlock()
	if r, _, e := m.Acquire(context.Background(), resources["claude-code"]); e == nil {
		r.Close()
		t.Fatal("application bypassed global reader limit")
	}
	old.Close()
	m.Close()
	reopened, e := NewApplications(dir, db, clients)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	if e = reopened.Cleanup(job.ID); e == nil {
		t.Fatal("completed cleanup snapshot was replayed")
	}
	for id := range clients {
		if !bytes.Equal(collect(t, reopened, resources[id]), payload) {
			t.Fatal("restart or old snapshot damaged an app", id)
		}
	}
	if counts[0].Load() != 2 || counts[1].Load() != 1 {
		t.Fatal("restart/old cleanup forced valid objects to download again")
	}
}
