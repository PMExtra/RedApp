package download

import (
	"bytes"
	"context"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/store/storetest"
	"github.com/PMExtra/RedApp/internal/testutil"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestDiskFailureDoesNotPoisonVerifiedCache(t *testing.T) {
	data := []byte("verified-cache")
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	m, _, _ := setup(t, c)
	good := authorizedResource(t, m, c, data)
	collect(t, m, good)
	bad := good
	bad.Source = testutil.SourceURL(c, "second")
	bad.Version = "0.2.0"
	bad.Hash = digest([]byte("different approved content"))
	bad.ID = LogicalIdentity(bad.Application, bad.Version, bad.Key)
	authorize(t, m, bad)
	m.mu.Lock()
	g, e := m.createLocked(bad, false)
	if e != nil {
		t.Fatal(e)
	}
	g.body.CloseFile()
	full, e := os.OpenFile("/dev/full", os.O_RDWR, 0600)
	if e != nil {
		m.mu.Unlock()
		t.Skip("/dev/full unavailable")
	}
	g.body.SetFile(full)
	m.mu.Unlock()
	rd, _, e := m.Acquire(context.Background(), bad)
	if e != nil {
		t.Fatal(e)
	}
	_, e = io.ReadAll(rd)
	rd.Close()
	if e == nil {
		t.Fatal("disk failure reported as success")
	}
	if !bytes.Equal(collect(t, m, good), data) {
		t.Fatal("existing complete generation was damaged")
	}
}
func TestDatabaseBusyIsBoundedAndVerifiedCacheSurvives(t *testing.T) {
	data := []byte("database-busy")
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	dir := t.TempDir()
	db := openStore(t, dir, store.WithBusyTimeout(30*time.Millisecond))
	m, e := newTestManager(dir, db, c)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { m.Close(); db.Close() })
	good := authorizedResource(t, m, c, data)
	collect(t, m, good)
	blocker := storetest.Open(t, dir)
	bad := good
	bad.Source = testutil.SourceURL(c, "second")
	bad.Version = "0.2.0"
	bad.Hash = digest([]byte("different approved content"))
	bad.ID = LogicalIdentity(bad.Application, bad.Version, bad.Key)
	authorize(t, m, bad)
	if _, e = blocker.Exec("BEGIN IMMEDIATE"); e != nil {
		t.Fatal(e)
	}
	defer blocker.Exec("ROLLBACK")
	start := time.Now()
	if _, _, e = m.Acquire(context.Background(), bad); e == nil {
		t.Fatal("busy database did not reject a new cache write")
	}
	if time.Since(start) > time.Second {
		t.Fatal("busy database caused an unbounded wait")
	}
	if !bytes.Equal(collect(t, m, good), data) {
		t.Fatal("busy database affected an existing complete generation")
	}
}
