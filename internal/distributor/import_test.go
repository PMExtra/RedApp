package distributor

import (
	"context"
	"errors"
	"github.com/PMExtra/RedApp/internal/networkproxy"
	"github.com/PMExtra/RedApp/internal/store"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestImportCancellationBeforeResponseHeadersRemainsCancellation(t *testing.T) {
	pool := NewPool()
	defer pool.CloseIdleConnections()
	const appUID, vendorUID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	plan, err := pool.PrepareConfiguration(store.DirectorySnapshot{GlobalProxy: networkproxy.Direct(), ProxyScopes: map[string]networkproxy.Scope{appUID: {VendorUID: vendorUID, Allowed: true, Proxy: networkproxy.Effective{Config: networkproxy.Direct()}}}})
	if err != nil {
		t.Fatal(err)
	}
	plan.Publish()
	started := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer upstream.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := pool.FetchImport(ctx, appUID, vendorUID, upstream.URL+"?sig=private-token")
		finished <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("import did not start")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "private-token") {
			t.Fatal("cancellation masked or source exposed", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled import stayed blocked")
	}
}

func TestImportSignedQueryRedirectLimitsAndSanitizedFailure(t *testing.T) {
	pool := NewPool()
	defer pool.CloseIdleConnections()
	const appUID, vendorUID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	plan, err := pool.PrepareConfiguration(store.DirectorySnapshot{GlobalProxy: networkproxy.Direct(), ProxyScopes: map[string]networkproxy.Scope{appUID: {VendorUID: vendorUID, Allowed: true, Proxy: networkproxy.Effective{Config: networkproxy.Direct()}}}})
	if err != nil {
		t.Fatal(err)
	}
	plan.Publish()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "sig=private-token" || r.Header.Get("Accept-Encoding") != "identity" || r.Header.Get("Authorization") != "" {
			t.Error("import request boundary")
		}
		io.WriteString(w, "bytes")
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/file?sig=private-token", 302)
	}))
	defer redirect.Close()
	res, err := pool.FetchImport(context.Background(), appUID, vendorUID, redirect.URL)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(data) != "bytes" {
		t.Fatal(string(data))
	}
	looping := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/loop?sig=private-token", 302) }))
	defer looping.Close()
	encoded := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Write([]byte("bad"))
	}))
	defer encoded.Close()
	for _, raw := range []string{looping.URL, encoded.URL, "file:///etc/passwd", "https://user:private-password@example.com/a?sig=private-token", "https://example.com/a#private-fragment"} {
		r, err := pool.FetchImport(context.Background(), appUID, vendorUID, raw)
		if r != nil {
			r.Body.Close()
		}
		if !errors.Is(err, ErrImport) || strings.Contains(err.Error(), "private-") {
			t.Fatal(raw, err)
		}
	}
}
