package download

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"github.com/PMExtra/RedApp/internal/testutil"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestUpstreamPayloadCountsCompressedArtifactOnce(t *testing.T) {
	plain := bytes.Repeat([]byte("compressible data\n"), 1000)
	var body bytes.Buffer
	zw := gzip.NewWriter(&body)
	zw.Write(plain)
	zw.Close()
	data := body.Bytes()
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "identity" {
			t.Error("unexpected HTTP compression negotiation")
		}
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(data)
	}))
	m, db, _ := setup(t, c)
	r := authorizedResource(t, m, c, data)
	for range 2 {
		if !bytes.Equal(collect(t, m, r), data) {
			t.Fatal("artifact was decompressed or corrupted")
		}
	}
	// Lowering deployment limits must also bound completed hits and same-app blob reuse.
	path := m.current[r.ID].Path
	if err := m.ConfigureLimits(16, 512, int64(len(data)-1)); err != nil {
		t.Fatal(err)
	}
	if reader, _, err := m.Acquire(context.Background(), r); !errors.Is(err, ErrArtifactLimit) {
		if reader != nil {
			reader.Close()
		}
		t.Fatal("complete hit bypassed artifact limit", err)
	}
	second := r
	second.Version = "0.2.0"
	second.ID = LogicalIdentity(second.Application, second.Version, second.Key)
	authorize(t, m, second)
	if reader, _, err := m.Acquire(context.Background(), second); !errors.Is(err, ErrArtifactLimit) {
		if reader != nil {
			reader.Close()
		}
		t.Fatal("same-application blob reuse bypassed artifact limit", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("limit reduction deleted a complete blob", err)
	}
	if len(m.Snapshot()) != 1 {
		t.Fatal("rejected reuse created a generation")
	}
	if err := m.ConfigureLimits(16, 512, 4<<30); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(collect(t, m, r), data) {
		t.Fatal("raising the limit did not restore cached serving")
	}
	counters, _ := db.Counters()
	if counters["upstream_bytes"] != int64(len(data)) || int64(len(data)) >= int64(len(plain)) {
		t.Fatal(counters, len(data), len(plain))
	}
}
func TestUpstreamPayloadIncludesFailedWritesAndRetries(t *testing.T) {
	data := []byte("payload read before disk rejection")
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	m, db, _ := setup(t, c)
	g, err := m.createLocked(authorizedResource(t, m, c, data), false)
	if err != nil {
		t.Fatal(err)
	}
	g.file.Close()
	g.file, err = os.OpenFile("/dev/full", os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	db.Add("upstream_bytes", 100) // Existing cumulative values must be retained.
	for attempt := int64(1); attempt <= 2; attempt++ {
		if err = m.attempt(g); err == nil || !strings.Contains(err.Error(), "Disk write") {
			t.Fatal(err)
		}
		counters, _ := db.Counters()
		if counters["upstream_bytes"] != 100+attempt*int64(len(data)) || g.SourceBytes != attempt*int64(len(data)) || g.Bytes != 0 {
			t.Fatal(counters, g.SourceBytes, g.Bytes)
		}
	}
}

func TestUpstreamPayloadCountsBodyRejectedByLengthLimit(t *testing.T) {
	data := []byte("payload larger than the configured limit")
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush() // Unknown length until the application consumes the body.
		w.Write(data)
	}))
	m, db, _ := setup(t, c)
	m.maxBytes = 1
	g, err := m.createLocked(authorizedResource(t, m, c, data), false)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.attempt(g); err == nil || !strings.Contains(err.Error(), "length exceeds") {
		t.Fatal(err)
	}
	counters, err := db.Counters()
	if err != nil || counters["upstream_bytes"] != int64(len(data)) || g.SourceBytes != int64(len(data)) || g.Bytes != 0 {
		t.Fatal(counters, g.SourceBytes, g.Bytes, err)
	}
}
