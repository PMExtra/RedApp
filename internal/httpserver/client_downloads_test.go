package httpserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
)

// A client (an IPv4 address or IPv6 /64) holding its download slots cannot
// start another download, while other clients still can.
func TestConcurrentDownloadsAreLimitedPerClient(t *testing.T) {
	h := newHarness(t, withMaxDownloadsPerClient(1))
	started, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(resume) }) }
	t.Cleanup(finish)
	h.upstreamProxy(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow.bin" {
			w.Header().Set("Content-Length", "10")
			io.WriteString(w, "first")
			w.(http.Flusher).Flush()
			close(started)
			<-resume
			io.WriteString(w, "-last")
			return
		}
		io.WriteString(w, "fast")
	}))
	h.createVendor("enterprise")
	h.createApp("enterprise", "files", application.HttpCache, map[string]any{"base_url": fixtureUpstream})
	get := func(path, remote string) *http.Request {
		r := httptest.NewRequest("GET", "http://redapp.test/enterprise/files/"+path, nil)
		r.RemoteAddr = remote
		return r
	}
	held := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.server.ServeHTTP(held, get("slow.bin", "[2001:db8::1]:40000"))
	}()
	<-started
	// Another address in the same /64 is the same client.
	if w := h.serve(get("fast.bin", "[2001:db8::2]:40001")); w.Code != 503 || errorCodeOf(t, w.Body.Bytes()) != string(codeTransferCapacity) {
		t.Fatal("second download of a busy client", w.Code, w.Body.String())
	}
	if w := h.serve(get("fast.bin", "192.0.2.7:40002")); w.Code != 200 || w.Body.String() != "fast" {
		t.Fatal("download of another client", w.Code, w.Body.String())
	}
	finish()
	<-done
	if held.Code != 200 || held.Body.String() != "first-last" {
		t.Fatal("held download", held.Code, held.Body.String())
	}
	// A finished download returns its slot.
	if w := h.serve(get("fast.bin", "[2001:db8::2]:40003")); w.Code != 200 {
		t.Fatal("download after the slot was released", w.Code, w.Body.String())
	}
}
