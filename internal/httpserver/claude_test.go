package httpserver

import (
	"bytes"
	"github.com/PMExtra/RedApp/internal/apps/claude"
	"github.com/PMExtra/RedApp/internal/store"
	"github.com/PMExtra/RedApp/internal/testutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestClaudeHTTPPreservesSignedBytesAndRejectsOtherResources(t *testing.T) {
	raw, e := os.ReadFile("../apps/claude/testdata/manifest.json")
	if e != nil {
		t.Fatal(e)
	}
	sig, _ := os.ReadFile("../apps/claude/testdata/manifest.json.sig")
	c, _ := testutil.Upstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest", "/stable":
			w.Write([]byte("2.1.285"))
		case "/2.1.285/manifest.json":
			w.Write(raw)
		case "/2.1.285/manifest.json.sig":
			w.Write(sig)
		default:
			t.Errorf("unauthorized upstream request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	dir := t.TempDir()
	db, e := store.Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.DB.Close()
	s := &Server{DB: db, Claude: claude.New(db, c), Dir: dir}
	for _, name := range []string{"install.sh", "install.ps1"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", "https://internal.example/claude-code/"+name, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "https://internal.example/claude-code") || strings.Contains(w.Body.String(), "@REDAPP_BASE_URL@") || strings.Contains(w.Body.String(), "https://downloads.claude.ai") {
			t.Fatalf("installer origin: %d", w.Code)
		}
	}
	for _, tc := range []struct {
		path   string
		status int
		body   []byte
	}{{"latest", 200, []byte("2.1.285\n")}, {"stable", 200, []byte("2.1.285\n")}, {"2.1.285/manifest.json", 200, raw}, {"2.1.285/manifest.json.sig", 200, sig}, {"2.1.285/manifest.zst.json", 404, nil}, {"2.1.285/linux-x64/claude.zst", 404, nil}, {"2.1.285/other/claude", 404, nil}, {"2.1.285/linux-x64/claude.exe", 404, nil}, {"../manifest.json", 404, nil}, {"latest?url=https://evil.example", 400, nil}} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", "https://internal.example/claude-code/"+tc.path, nil))
		if w.Code != tc.status || tc.body != nil && !bytes.Equal(w.Body.Bytes(), tc.body) {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("POST", "https://internal.example/claude-code/latest", nil))
	if w.Code != 405 {
		t.Fatal("unexpected write endpoint")
	}
}
