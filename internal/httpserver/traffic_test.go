package httpserver

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type compressedResponse struct {
	http.ResponseWriter
	zip *gzip.Writer
}

type failedResponse struct{ http.ResponseWriter }

func (w failedResponse) Write(p []byte) (int, error) { return 3, io.ErrClosedPipe }

func (w compressedResponse) Write(p []byte) (int, error) { return w.zip.Write(p) }
func (w compressedResponse) Flush()                      { w.zip.Flush(); w.ResponseWriter.(http.Flusher).Flush() }
func TestDownstreamCountersPrecedeExternalHTTPCompression(t *testing.T) {
	data := bytes.Repeat([]byte("repeated fixture payload\n"), 5000)
	h := newHarness(t)
	h.upstreamProxy(codexRelease("0.159.2", map[string][]byte{"asset.tgz": data}, nil))
	key := h.releaseApp("fixture", "codex", "codex")
	handler, db := h.server, h.store
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		zip := gzip.NewWriter(w)
		defer zip.Close()
		handler.ServeHTTP(compressedResponse{w, zip}, r)
	}))
	defer server.Close()
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	defer client.CloseIdleConnections()
	var transferred int
	for range 2 {
		response, err := client.Get(server.URL + "/" + key + "/releases/0.159.2/asset.tgz")
		if err != nil {
			t.Fatal(err)
		}
		wire, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		transferred += len(wire)
		zip, err := gzip.NewReader(bytes.NewReader(wire))
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := io.ReadAll(zip)
		zip.Close()
		if err != nil || !bytes.Equal(decoded, data) {
			t.Fatal("compressed delivery changed artifact", err)
		}
	}
	counters, _ := db.Counters()
	if counters["downstream_bytes"] != int64(2*len(data)) || counters["upstream_bytes"] != int64(len(data)) || transferred >= 2*len(data) {
		t.Fatal(counters, transferred)
	}
	// A client-side write failure counts only bytes accepted by ResponseWriter.
	handler.ServeHTTP(failedResponse{httptest.NewRecorder()}, httptest.NewRequest("GET", "http://internal/"+key+"/releases/0.159.2/asset.tgz", nil))
	counters, err := db.Counters()
	if err != nil || counters["downstream_bytes"] != int64(2*len(data)+3) || counters["download_errors"] != 1 || counters["upstream_bytes"] != int64(len(data)) {
		t.Fatal(counters, err)
	}
}
