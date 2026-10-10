package httpserver

import (
	"bytes"
	"io"
	"net/http"
	"testing"
)

func TestDownloadCompletesWhenCounterPersistenceFails(t *testing.T) {
	data := bytes.Repeat([]byte("official archive "), 8<<10) // Several copy-buffer chunks.
	h := newHarness(t)
	h.upstreamProxy(codexRelease("0.159.2", map[string][]byte{"archive.tgz": data}, nil))
	key := h.releaseApp("fixture", "codex", "codex")
	db := h.store
	triggers := []string{
		`CREATE TRIGGER fail_counter_insert BEFORE INSERT ON metric_counters BEGIN SELECT RAISE(FAIL,'counter fault'); END`,
		`CREATE TRIGGER fail_counter_update BEFORE UPDATE ON metric_counters BEGIN SELECT RAISE(FAIL,'counter fault'); END`,
		`CREATE TRIGGER fail_version_update BEFORE UPDATE ON app_versions BEGIN SELECT RAISE(FAIL,'counter fault'); END`,
	}
	for _, trigger := range triggers {
		if _, err := h.sql().Exec(trigger); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 { // Miss then cache hit.
		response, err := http.Get(h.http.URL + "/" + key + "/releases/0.159.2/archive.tgz")
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || !bytes.Equal(body, data) {
			t.Fatal("counter failure aborted the download", response.StatusCode, len(body), err)
		}
	}
	if err := db.FlushCounters(); err == nil {
		t.Fatal("injected counter fault not observed")
	}
	for _, name := range []string{"fail_counter_insert", "fail_counter_update", "fail_version_update"} {
		h.sql().Exec("DROP TRIGGER " + name)
	}
	// Increments retained across failed flushes are persisted once storage recovers.
	entry, _ := h.server.registry.Lookup(key)
	counters, _ := db.CountersFor(entry.MetricsID())
	stats, _ := db.VersionStats(entry.StorageID())
	if counters["artifact_requests"] != 2 || counters["download_success"] != 2 || counters["downstream_bytes"] != int64(2*len(data)) || stats["0.159.2"].DownstreamBytes != int64(2*len(data)) {
		t.Fatal(counters, stats)
	}
}
