package httpserver

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
)

// A cold HTTP cache file reaches the client through the whole server stack
// while the upstream response is still incomplete, and is cached afterwards.
func TestHTTPCacheMissStreamsThroughTheServer(t *testing.T) {
	first := bytes.Repeat([]byte("a"), 64<<10)
	rest := bytes.Repeat([]byte("b"), 64<<10)
	release := make(chan struct{})
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Length", fmt.Sprint(len(first)+len(rest)))
		w.Write(first)
		w.(http.Flusher).Flush()
		select {
		case <-release:
			w.Write(rest)
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	h := newHarness(t)
	h.createVendor("enterprise")
	h.createApp("enterprise", "files", application.HttpCache, map[string]any{"base_url": upstream.URL + "/packages"})
	response, err := h.client.Get(h.http.URL + "/enterprise/files/big.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength != int64(len(first)+len(rest)) || response.Header.Get("ETag") != `"v1"` {
		t.Fatal("streamed response headers", response.StatusCode, response.ContentLength, response.Header)
	}
	head := make([]byte, len(first))
	if _, err = io.ReadFull(response.Body, head); err != nil || !bytes.Equal(head, first) || calls.Load() != 1 {
		t.Fatal("first bytes did not arrive before the upstream finished", err, calls.Load())
	}
	close(release)
	tail, err := io.ReadAll(response.Body)
	if err != nil || !bytes.Equal(tail, rest) {
		t.Fatal("rest of the streamed file", len(tail), err)
	}
	cached, _ := h.request("GET", "/enterprise/files/big.bin", nil, http.StatusOK, nil)
	if !bytes.Equal(cached, append(append([]byte{}, first...), rest...)) || calls.Load() != 1 {
		t.Fatal("streamed file was not served from the cache", len(cached), calls.Load())
	}
}
