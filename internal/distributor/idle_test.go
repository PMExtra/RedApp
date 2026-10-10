package distributor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func idleFixture(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	c, err := NewPool().NewClient(server.URL+"/root", GeneralHTTP)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestStalledBodyFailsWithRetryableIdleTimeout(t *testing.T) {
	c := idleFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "8")
		w.Write([]byte("part"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	resp, err := c.Send(context.Background(), Request{Method: http.MethodGet, URL: sourceURL(c, "asset"), IdleTimeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if string(data) != "part" || !errors.Is(err, ErrIdleTimeout) || !errors.Is(err, ErrConnection) || Classify(err) != KindTimeout {
		t.Fatalf("stalled body: data=%q err=%v", data, err)
	}
}

func TestProgressingBodyHasNoOverallDeadline(t *testing.T) {
	const idle = 300 * time.Millisecond
	chunk := bytes.Repeat([]byte("x"), 1024)
	const chunks = 40 // about 1s in total: several idle windows, never one idle gap
	c := idleFixture(t, func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < chunks; i++ {
			w.Write(chunk)
			w.(http.Flusher).Flush()
			time.Sleep(idle / 12) // upstream pacing, not test synchronization
		}
	})
	start := time.Now()
	resp, err := c.Send(context.Background(), Request{Method: http.MethodGet, URL: sourceURL(c, "asset"), IdleTimeout: idle})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	n, err := io.Copy(io.Discard, resp.Body)
	if err != nil || n != int64(chunks*len(chunk)) {
		t.Fatalf("progressing body failed: n=%d err=%v", n, err)
	}
	if elapsed := time.Since(start); elapsed < 2*idle {
		t.Fatal("fixture did not outlast the idle timeout", elapsed)
	}
}
