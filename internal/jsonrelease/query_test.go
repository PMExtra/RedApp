package jsonrelease

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/PMExtra/RedApp/internal/releaseconfig"
)

func TestQueryTransformsNestedVendorAliases(t *testing.T) {
	r := NewRunner()
	input := json.RawMessage(`{"body":{"version":"2026.2","downloads":{"windows":{"link":"https://cdn.example/a"},"linuxARM64":{"link":"https://cdn.example/b"}}},"context":{}}`)
	expression := `{version: .body.version, artifacts: [.body.downloads | to_entries[] | {key: .key, os: (if .key == "windows" then "windows" else "linux" end), arch: (if .key == "linuxARM64" then "arm64" else "x64" end), url: .value.link}]}`
	raw, err := r.Run(context.Background(), expression, input)
	if err != nil {
		t.Fatal(err)
	}
	var result struct{ Artifacts []releaseconfig.Artifact }
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 2 {
		t.Fatal(string(raw))
	}
	found := false
	for _, a := range result.Artifacts {
		if a.OS == "linux" && a.Arch == "arm64" {
			found = true
		}
	}
	if !found {
		t.Fatal(string(raw))
	}
}
func TestQueryNoExternalCapabilitiesAndStrictResults(t *testing.T) {
	runner := NewRunner()
	for _, expression := range []string{`import "anything" as x; x::read`, `input`, `1,2`, `empty`, `.broken[`, `error("fixture")`} {
		if _, err := runner.Run(context.Background(), expression, json.RawMessage(`{}`)); err == nil {
			t.Fatal("unexpected success", expression)
		}
	}
	raw, err := runner.Run(context.Background(), `env`, json.RawMessage(`{}`))
	if err != nil || string(raw) != "{}" {
		t.Fatal("environment exposed", string(raw), err)
	}
}
func TestQueryCancellationAndBounds(t *testing.T) {
	runner := NewRunner()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := runner.Run(ctx, `def repeat: repeat; repeat`, json.RawMessage(`{}`)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), strings.Repeat(".", releaseconfig.MaxQueryBytes+1), json.RawMessage(`{}`)); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	if _, err := runner.Run(context.Background(), `"x" * 4194305`, json.RawMessage(`{}`)); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	// Cancellation releases capacity; subsequent ordinary queries must work.
	if _, err := runner.Run(context.Background(), `.`, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
}
