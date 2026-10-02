package download

import (
	"bytes"
	"context"
	"database/sql"
	"github.com/PMExtra/RedApp/internal/testutil"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
	bad.Source = c.URL("second")
	bad.Version = "0.2.0"
	bad.Hash = digest([]byte("different approved content"))
	bad.ID = LogicalIdentity(bad.Application, bad.Version, bad.Key)
	authorize(t, m, bad)
	m.mu.Lock()
	g, e := m.createLocked(bad, false)
	if e != nil {
		t.Fatal(e)
	}
	g.file.Close()
	g.file, e = os.OpenFile("/dev/full", os.O_RDWR, 0600)
	if e != nil {
		m.mu.Unlock()
		t.Skip("/dev/full 不可用")
	}
	m.mu.Unlock()
	rd, _, e := m.Acquire(context.Background(), bad)
	if e != nil {
		t.Fatal(e)
	}
	_, e = io.ReadAll(rd)
	rd.Close()
	if e == nil {
		t.Fatal("磁盘失败被当作成功")
	}
	if !bytes.Equal(collect(t, m, good), data) {
		t.Fatal("已有 complete 受污染")
	}
}
func TestDatabaseBusyIsBoundedAndVerifiedCacheSurvives(t *testing.T) {
	data := []byte("database-busy")
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	m, db, dir := setup(t, c)
	good := authorizedResource(t, m, c, data)
	collect(t, m, good)
	if _, e := db.DB.Exec("PRAGMA busy_timeout=30"); e != nil {
		t.Fatal(e)
	}
	blocker, e := sql.Open("sqlite3", filepath.Join(dir, "state.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Close()
	bad := good
	bad.Source = c.URL("second")
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
		t.Fatal("数据库 busy 未拒绝新缓存写入")
	}
	if time.Since(start) > time.Second {
		t.Fatal("数据库 busy 无界等待")
	}
	if !bytes.Equal(collect(t, m, good), data) {
		t.Fatal("已有 complete 受数据库 busy 影响")
	}
}
