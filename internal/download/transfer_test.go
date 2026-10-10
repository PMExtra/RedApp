package download

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/testutil"
)

func TestSlowProgressingDownloadOutlastsIdleTimeout(t *testing.T) {
	data := bytes.Repeat([]byte("slow-but-alive "), 4096)
	const idle = 300 * time.Millisecond
	const chunks = 40 // about 1s in total, never one idle gap
	var requests atomic.Int32
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		size := (len(data) + chunks - 1) / chunks
		for offset := 0; offset < len(data); offset += size {
			w.Write(data[offset:min(offset+size, len(data))])
			w.(http.Flusher).Flush()
			time.Sleep(idle / 12) // upstream pacing, not test synchronization
		}
	}))
	m, _, _ := setup(t, c)
	m.idleTimeout = idle
	start := time.Now()
	if got := collect(t, m, authorizedResource(t, m, c, data)); !bytes.Equal(got, data) {
		t.Fatal("slow download content mismatch")
	}
	if requests.Load() != 1 || time.Since(start) < 2*idle {
		t.Fatalf("slow download was retried or did not outlast the idle timeout: requests=%d", requests.Load())
	}
}

// stallingUpstream serves half of data, then stalls while stall is set.
// Range requests are answered from the requested offset.
type stallingUpstream struct {
	data   []byte
	stall  atomic.Bool
	mu     sync.Mutex
	ranges []string
}

func (u *stallingUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	u.ranges = append(u.ranges, r.Header.Get("Range"))
	u.mu.Unlock()
	half := len(u.data) / 2
	w.Header().Set("ETag", "\"stable\"")
	w.Header().Set("Accept-Ranges", "bytes")
	body := u.data
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		var start int
		if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-", &start); err != nil || r.Header.Get("If-Range") != "\"stable\"" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(u.data)-1, len(u.data)))
		w.Header().Set("Content-Length", fmt.Sprint(len(u.data)-start))
		w.WriteHeader(http.StatusPartialContent)
		body = u.data[start:]
	} else {
		w.Header().Set("Content-Length", fmt.Sprint(len(u.data)))
		w.WriteHeader(http.StatusOK)
	}
	if !u.stall.Load() {
		w.Write(body)
		return
	}
	if len(body) == len(u.data) {
		w.Write(body[:half])
	}
	w.(http.Flusher).Flush()
	<-r.Context().Done() // stalls until the client gives up on the idle body
}

func (u *stallingUpstream) requests() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.ranges...)
}

func TestStalledBodyRetriesWithRange(t *testing.T) {
	upstream := &stallingUpstream{data: bytes.Repeat([]byte("resumable "), 2000)}
	upstream.stall.Store(true)
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only the first request stalls; the retry completes.
		if len(upstream.requests()) == 1 {
			upstream.stall.Store(false)
		}
		upstream.ServeHTTP(w, r)
	}))
	m, _, _ := setup(t, c)
	m.idleTimeout = 100 * time.Millisecond
	if got := collect(t, m, authorizedResource(t, m, c, upstream.data)); !bytes.Equal(got, upstream.data) {
		t.Fatal("retried download content mismatch")
	}
	want := fmt.Sprintf("bytes=%d-", len(upstream.data)/2)
	if ranges := upstream.requests(); len(ranges) != 2 || ranges[0] != "" || ranges[1] != want {
		t.Fatal("idle timeout did not retry with Range", ranges)
	}
}

func TestExhaustedRetriesKeepResumablePart(t *testing.T) {
	upstream := &stallingUpstream{data: bytes.Repeat([]byte("keep-prefix "), 2000)}
	upstream.stall.Store(true)
	c, _ := testutil.Upstream(t, upstream)
	m, _, _ := setup(t, c)
	m.idleTimeout = 100 * time.Millisecond
	m.retry.Attempts = 2
	r := authorizedResource(t, m, c, upstream.data)
	rd, _, err := m.Acquire(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.ReadAll(rd); err == nil {
		t.Fatal("stalled download reported success")
	}
	rd.Close()
	half := int64(len(upstream.data) / 2)
	views := m.Snapshot()
	if len(views) != 1 || views[0].State != "interrupted" || views[0].Bytes != half || !views[0].Current || views[0].Error != "upstream download interrupted" {
		t.Fatalf("exhausted retries did not keep the resumable part: %+v", views)
	}
	if st, err := os.Stat(views[0].Path); err != nil || st.Size() < half {
		t.Fatal("resumable part was deleted", err)
	}
	upstream.stall.Store(false)
	if got := collect(t, m, r); !bytes.Equal(got, upstream.data) {
		t.Fatal("resumed download content mismatch")
	}
	want := fmt.Sprintf("bytes=%d-", half)
	if ranges := upstream.requests(); len(ranges) != 3 || ranges[2] != want {
		t.Fatal("later admission did not resume with Range", ranges)
	}
	if after := m.Snapshot(); len(after) != 1 || after[0].ID != views[0].ID || after[0].State != "complete" {
		t.Fatalf("resume did not complete the retained generation: %+v", after)
	}
}

func TestHashMismatchDiscardsResumableData(t *testing.T) {
	data := bytes.Repeat([]byte("approved "), 1000)
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", "\"stable\"")
		w.Header().Set("Accept-Ranges", "bytes")
		w.Write(bytes.ToUpper(data))
	}))
	m, _, _ := setup(t, c)
	rd, _, err := m.Acquire(context.Background(), authorizedResource(t, m, c, data))
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(rd)
	rd.Close()
	if err == nil {
		t.Fatal("hash mismatch served as success")
	}
	for _, view := range m.Snapshot() {
		if view.State != "deleted" && (view.Current || view.State == "interrupted") {
			t.Fatal("integrity failure kept data for resume", view)
		}
	}
}
