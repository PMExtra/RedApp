package httpserver

import (
	"bytes"
	"strings"
	"testing"
)

func TestInstructionsDocumentRunsInOpaqueOriginSandbox(t *testing.T) {
	h := newHarness(t)
	data, headers := h.request("GET", "/api/apps/openai/codex/instructions/document?lang=en", nil, 200, nil)
	policy := strings.Split(headers.Get("Content-Security-Policy"), ";")
	if strings.TrimSpace(policy[0]) != "sandbox allow-scripts allow-popups allow-popups-to-escape-sandbox allow-top-navigation-by-user-activation" {
		t.Fatal("document is not sandboxed to an opaque origin", policy)
	}
	for _, want := range []string{"script-src * 'unsafe-inline' 'unsafe-eval' data: blob:", "frame-ancestors 'self'"} {
		found := false
		for _, directive := range policy {
			found = found || strings.TrimSpace(directive) == want
		}
		if !found {
			t.Fatal("document policy lost", want, policy)
		}
	}
	if strings.Contains(headers.Get("Content-Security-Policy"), "allow-same-origin") || headers.Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Fatal(headers)
	}
	if !bytes.Contains(data, []byte(`<base target="_top">`)) || !bytes.Contains(data, []byte("redapp-instructions-height")) {
		t.Fatal("document lost top navigation or height reporting")
	}
	if !bytes.Contains(data, []byte(`data-theme="light"`)) {
		t.Fatal("document does not default to the light theme")
	}
	data, _ = h.request("GET", "/api/apps/openai/codex/instructions/document?lang=en&theme=dark", nil, 200, nil)
	if !bytes.Contains(data, []byte(`data-theme="dark"`)) {
		t.Fatal("document ignores the dark theme")
	}
	h.request("GET", "/api/apps/openai/codex/instructions/document?lang=en&theme=blue", nil, 400, nil)
	// The opaque-origin document loads the code font through CORS.
	_, headers = h.request("GET", "/assets/JetBrainsMono-Regular-v2.304.woff2", nil, 200, nil)
	if headers.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("font is not available to the sandboxed document", headers)
	}
}
