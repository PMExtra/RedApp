package download

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"github.com/PMExtra/RedApp/internal/distributor"
	"github.com/PMExtra/RedApp/internal/spool"
	"github.com/PMExtra/RedApp/internal/testutil"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestEffectiveAverageExcludesVerificationAndRecentSnapshot(t *testing.T) {
	data := bytes.Repeat([]byte("speed"), 10000)
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(data) }))
	m, db, dir := setup(t, c)
	collect(t, m, authorizedResource(t, m, c, data))
	m.mu.Lock()
	g := m.current[authorizedResource(t, m, c, data).ID]
	now := time.Now()
	g.Started = now.Add(-10 * time.Second)
	g.Received = now.Add(-8 * time.Second)
	g.Finished = now
	g.VerificationNS = 8 * time.Second.Nanoseconds()
	g.samples = []sample{{now.Add(-2 * time.Second), 0}, {now, 50000}}
	g.SourceBytes = 50000
	if e := m.save(g); e != nil {
		m.mu.Unlock()
		t.Fatal(e)
	}
	m.mu.Unlock()
	v := m.Snapshot()[0]
	if v.AverageBPS != 25000 || v.DownloadNS != 2*time.Second.Nanoseconds() {
		t.Fatalf("average speed includes verification time: %+v", v)
	}
	if math.Abs(v.RecentBPS-25000) > 200 {
		t.Fatal("wrong recent speed in snapshot", v.RecentBPS)
	}
	if v.SampledAt.IsZero() {
		t.Fatal("sample time missing")
	}
	if e := m.Close(); e != nil {
		t.Fatal(e)
	}
	restored, e := newTestManager(dir, db, c)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	after := restored.Snapshot()[0]
	if after.DownloadNS != v.DownloadNS || after.AverageBPS != v.AverageBPS {
		t.Fatal("restart changed precise verified download duration", after)
	}
}
func TestHangingUpstreamHasBoundedFailure(t *testing.T) {
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	c.HTTP.Timeout = 30 * time.Millisecond
	m, _, _ := setup(t, c)
	data := []byte("not received")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rd, _, e := m.Acquire(ctx, authorizedResource(t, m, c, data))
	if e != nil {
		t.Fatal(e)
	}
	defer rd.Close()
	_, e = io.ReadAll(rd)
	if e == nil || ctx.Err() != nil {
		t.Fatal("hanging upstream did not stop at the retry bound", e)
	}
}

func TestFailureCategoriesFollowErrorTypes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{errHashMismatch, "hash"},
		{unsafeResume, "range"},
		{distributor.ErrUnsafeEncoding, "encoding"},
		{&failure{message: "Disk write failed", category: "disk", cause: os.ErrPermission}, "disk"},
		{errLength, "length"},
		{errTruncated, "length"},
		{errBlobInvalid, "disk"},
		{spool.StatusError(503), "http"},
		{&failure{message: "Cache state commit failed", category: "database"}, "database"},
		{&net.DNSError{Err: "no such host", Name: "upstream.example"}, "dns"},
	} {
		if got := failureCategory(tc.err); got != tc.want {
			t.Errorf("%v: got %s want %s", tc.err, got, tc.want)
		}
	}
}

type failingTransport struct{ err error }

func (f failingTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, f.err }

func TestTransportFailureCategories(t *testing.T) {
	c, _ := testutil.Upstream(t, http.NotFoundHandler())
	for _, tc := range []struct {
		name  string
		cause error
		want  string
	}{
		{"dns", &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "upstream.example", IsNotFound: true}}, "dns"},
		{"certificate", x509.UnknownAuthorityError{}, "tls"},
		{"handshake", tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"}, "tls"},
		{"deadline", context.DeadlineExceeded, "timeout"},
		{"refused", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, "upstream"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c.HTTP.Transport = failingTransport{tc.cause}
			_, err := c.Get(context.Background(), testutil.SourceURL(c, "asset"), nil)
			if err == nil || err.Error() != "Upstream connection failed" {
				t.Fatal("transport failure message changed", err)
			}
			if got := failureCategory(err); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	// Body read failures keep their stable message and their transport class.
	interrupted := &failure{message: "Upstream download interrupted", category: "upstream", cause: &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}, transient: true}
	if got := failureCategory(interrupted); got != "timeout" {
		t.Fatal("interrupted read timeout category", got)
	}
}
