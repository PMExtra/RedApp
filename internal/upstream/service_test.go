package upstream

import (
	"errors"
	"testing"
)

func TestNormalizeOrigin(t *testing.T) {
	for raw, want := range map[string]string{"https://DMZ.example/": "https://dmz.example", " http://10.0.0.1:8080 ": "http://10.0.0.1:8080"} {
		if got, err := NormalizeOrigin(raw); err != nil || got != want {
			t.Errorf("%q: %q %v", raw, got, err)
		}
	}
	for _, raw := range []string{"ftp://a", "https://a/path", "https://u@a", "https://a?x=1", "https://a#f", "a.example"} {
		if _, err := NormalizeOrigin(raw); err == nil {
			t.Errorf("%q accepted", raw)
		}
	}
}

// An upstream that does not describe its JSON release channels cannot be
// mirrored, with an explicit reason instead of a configuration error.
func TestReleaseChannelsMustBeDescribed(t *testing.T) {
	beta := "beta"
	other := "other"
	for name, m := range map[string]mirrorApp{
		"no default":      {Provider: "json-release", Channels: []string{"beta", "latest"}, SourceEpoch: 1},
		"unknown default": {Provider: "json-release", Channels: []string{"beta", "latest"}, DefaultChannel: &other, SourceEpoch: 1},
		"no source epoch": {Provider: "json-release", Channels: []string{"beta", "latest"}, DefaultChannel: &beta},
		"only latest":     {Provider: "json-release", Channels: []string{"latest"}, DefaultChannel: &beta, SourceEpoch: 1},
	} {
		if err := checkReleaseChannels(m); !errors.Is(err, ErrIncompatible) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := checkReleaseChannels(mirrorApp{Provider: "json-release", Channels: []string{"beta", "latest", "stable"}, DefaultChannel: &beta, SourceEpoch: 3}); err != nil {
		t.Fatal(err)
	}
	if err := checkReleaseChannels(mirrorApp{Provider: "codex", Channels: []string{"latest"}}); err != nil {
		t.Fatal(err)
	}
}
