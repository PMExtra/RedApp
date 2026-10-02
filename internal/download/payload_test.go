package download

import (
	"bytes"
	"compress/gzip"
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
	r := resource(c, data)
	for range 2 {
		if !bytes.Equal(collect(t, m, r), data) {
			t.Fatal("artifact was decompressed or corrupted")
		}
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
	g, err := m.createLocked(resource(c, data), false)
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
	g, err := m.createLocked(resource(c, data), false)
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
