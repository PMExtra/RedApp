package logging

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestErrorMasksURLCredentials(t *testing.T) {
	for in, want := range map[string]string{
		"unexpected EOF": "unexpected EOF",
		"proxyconnect tcp: http://ops:secret@proxy.internal:3128 refused": "proxyconnect tcp: http://****@proxy.internal:3128 refused",
		`Get "https://user@example.test/a": socks5://u:p@10.0.0.1:1080`:   `Get "https://****@example.test/a": socks5://****@10.0.0.1:1080`,
	} {
		if got := Error(errors.New(in)).Value.String(); got != want {
			t.Errorf("Error(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestForTagsComponentAndDefaultsToDiscard(t *testing.T) {
	var buf bytes.Buffer
	For(slog.New(slog.NewTextHandler(&buf, nil)), "download").Warn("failed", Error(io.ErrUnexpectedEOF))
	if got := buf.String(); !strings.Contains(got, "component=download") || !strings.Contains(got, `error="unexpected EOF"`) {
		t.Fatal(got)
	}
	For(nil, "download").Error("discarded")
}
