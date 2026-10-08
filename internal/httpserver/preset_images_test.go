package httpserver

import (
	"github.com/PMExtra/RedApp/presets"
	"testing"
)

func TestPresetImageRoutes(t *testing.T) {
	h := newDirectoryHarness(t, t.TempDir())
	for _, relative := range []string{"assets/builtin/openai.svg", "assets/builtin/anthropic.svg", "assets/openai/codex/icon.svg", "assets/anthropic/claude-code/icon.svg"} {
		url, err := presets.Icon(relative)
		if err != nil {
			t.Fatal(err)
		}
		image, _ := presets.Embedded().Image(url)
		body, headers := h.request("GET", url, nil, 200, nil)
		if string(body) != string(image.Body) || headers.Get("Content-Type") != image.ContentType || headers.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("invalid preset image response", url, headers)
		}
		body, _ = h.request("HEAD", url, nil, 200, nil)
		if len(body) != 0 {
			t.Fatal("HEAD returned a body")
		}
	}
	h.request("GET", "/assets/presets/missing.svg", nil, 404, nil)
	h.request("GET", "/assets/presets/openai.yaml", nil, 404, nil)
}
