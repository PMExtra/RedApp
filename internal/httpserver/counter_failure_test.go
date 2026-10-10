package httpserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	app "github.com/PMExtra/RedApp/internal/apps/codex"
	"github.com/PMExtra/RedApp/internal/testutil"
)

func TestDownloadCompletesWhenCounterPersistenceFails(t *testing.T) {
	data := bytes.Repeat([]byte("official archive "), 8<<10) // Several copy-buffer chunks.
	h := sha256.Sum256(data)
	hash := hex.EncodeToString(h[:])
	var base string
	upstream, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "latest") || strings.HasSuffix(r.URL.Path, "release.json") {
			json.NewEncoder(w).Encode(app.Release{Tag: "rust-v0.159.2", Assets: []app.Asset{{Name: "archive.tgz", Digest: "sha256:" + hash, URL: base + "/releases/0.159.2/archive.tgz"}}})
		} else {
			w.Write(data)
		}
	}))
	base = upstream.Base.String()
	handler, db, _ := newTestServer(t, upstream)
	server := startTestServer(t, handler)
	triggers := []string{
		`CREATE TRIGGER fail_counter_insert BEFORE INSERT ON metric_counters BEGIN SELECT RAISE(FAIL,'counter fault'); END`,
		`CREATE TRIGGER fail_counter_update BEFORE UPDATE ON metric_counters BEGIN SELECT RAISE(FAIL,'counter fault'); END`,
		`CREATE TRIGGER fail_version_update BEFORE UPDATE ON app_versions BEGIN SELECT RAISE(FAIL,'counter fault'); END`,
	}
	for _, trigger := range triggers {
		if _, err := db.DB.Exec(trigger); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 { // Miss then cache hit.
		response, err := http.Get(server.URL + "/openai/codex/releases/0.159.2/archive.tgz")
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
		db.DB.Exec("DROP TRIGGER " + name)
	}
	// Increments retained across failed flushes are persisted once storage recovers.
	counters, _ := db.CountersFor("openai/codex")
	stats, _ := db.VersionStats("openai/codex")
	if counters["artifact_requests"] != 2 || counters["download_success"] != 2 || counters["downstream_bytes"] != int64(2*len(data)) || stats["0.159.2"].DownstreamBytes != int64(2*len(data)) {
		t.Fatal(counters, stats)
	}
}
