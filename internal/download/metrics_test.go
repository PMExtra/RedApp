package download

import (
	"bytes"
	"context"
	"github.com/PMExtra/RedApp/internal/testutil"
	"io"
	"math"
	"net/http"
	"testing"
	"time"
)

func TestEffectiveAverageExcludesVerificationAndRecentSnapshot(t *testing.T) {
	data := bytes.Repeat([]byte("speed"), 10000)
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	m, _, _ := setup(t, c)
	collect(t, m, resource(c, data))
	m.mu.Lock()
	g := m.current[resource(c, data).ID]
	now := time.Now()
	g.Started = now.Add(-10 * time.Second)
	g.Received = now.Add(-8 * time.Second)
	g.Finished = now
	g.VerificationNS = 8 * time.Second.Nanoseconds()
	g.samples = []sample{{now.Add(-2 * time.Second), 0}, {now, 50000}}
	g.SourceBytes = 50000
	m.mu.Unlock()
	v := m.Snapshot()[0]
	if v.AverageBPS != 25000 || v.DownloadNS != 2*time.Second.Nanoseconds() {
		t.Fatalf("平均速度包含验证耗时: %+v", v)
	}
	if math.Abs(v.RecentBPS-25000) > 200 {
		t.Fatal("近期快照速度错误", v.RecentBPS)
	}
	if v.SampledAt.IsZero() {
		t.Fatal("缺采样时间")
	}
}
func TestHangingUpstreamHasBoundedFailure(t *testing.T) {
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	c.HTTP.Timeout = 30 * time.Millisecond
	m, _, _ := setup(t, c)
	data := []byte("not received")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rd, _, e := m.Acquire(ctx, resource(c, data))
	if e != nil {
		t.Fatal(e)
	}
	defer rd.Close()
	_, e = io.ReadAll(rd)
	if e == nil || ctx.Err() != nil {
		t.Fatal("挂起上游未按请求重试边界终止", e)
	}
}

func TestEnglishFailureCategories(t *testing.T) {
	for message, want := range map[string]string{
		"Complete file SHA256 does not match":                  "hash",
		"Unsafe upstream resume; a new generation is required": "range",
		"Unsafe upstream Content-Encoding":                     "encoding",
		"Disk write failed":                                    "disk",
		"File fsync failed":                                    "disk",
		"Artifact length does not match":                       "length",
		"Artifact truncated":                                   "length",
		"Upstream HTTP 503":                                    "http",
		"DNS returned no addresses":                            "dns",
		"TLS handshake failed":                                 "tls",
		"request timeout":                                      "timeout",
		"Cache state commit failed":                            "database",
	} {
		if got := failureCategory(message); got != want {
			t.Errorf("%q: got %s want %s", message, got, want)
		}
	}
}
