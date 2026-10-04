package distributor

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestImportSignedQueryRedirectLimitsAndSanitizedFailure(t *testing.T) {
	pool := NewPool()
	defer pool.CloseIdleConnections()
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
	res, err := pool.FetchImport(context.Background(), redirect.URL)
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
		r, err := pool.FetchImport(context.Background(), raw)
		if r != nil {
			r.Body.Close()
		}
		if !errors.Is(err, ErrImport) || strings.Contains(err.Error(), "private-") {
			t.Fatal(raw, err)
		}
	}
}
