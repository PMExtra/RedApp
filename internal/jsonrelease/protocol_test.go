package jsonrelease

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/application"
)

func TestProtocolFreezesConfigurationAndVerifiesIdentity(t *testing.T) {
	calls := []string{}
	config := discoveryConfig()
	protocol, err := NewProtocol(config, Discoverer{Query: fixtureQuery, Fetch: fixtureFetch(&calls)})
	if err != nil {
		t.Fatal(err)
	}
	config.Sources.Artifacts[0].Origin = "https://evil.example"
	config.Channels["stable"].Requests[0].Context["key"] = "changed"
	resolved, err := protocol.ResolveChannel(context.Background(), "latest")
	if err != nil {
		t.Fatal(err)
	}
	release, err := protocol.VerifyRelease(resolved.Version, *resolved.Envelope)
	if err != nil || len(release.Artifacts) != 2 {
		t.Fatal(release, err)
	}
	if release.Artifacts[1].Key != "windows-x64-exe-user" {
		t.Fatal("configuration mutation reached protocol", release.Artifacts)
	}
	if _, err = protocol.VerifyRelease("9.9.9", *resolved.Envelope); err == nil {
		t.Fatal("mismatched snapshot identity accepted")
	}
	var body map[string]any
	if err = json.Unmarshal(resolved.Envelope.Raw, &body); err != nil {
		t.Fatal(err)
	}
	body["version"] = "2.0.0"
	raw, _ := json.Marshal(body)
	if _, err = protocol.VerifyRelease(resolved.Version, application.Envelope{Raw: raw}); err == nil {
		t.Fatal("changed version accepted")
	}
	if _, err = protocol.VerifyRelease(resolved.Version, application.Envelope{Raw: resolved.Envelope.Raw, Signature: []byte("unexpected")}); err == nil {
		t.Fatal("signature ignored")
	}
	if _, err = protocol.FetchRelease(context.Background(), resolved.Version); !errors.Is(err, application.ErrNotFound) {
		t.Fatal("old release rediscovered as latest", err)
	}
	if len(calls) != 4 {
		t.Fatal("unexpected extra upstream fetch", calls)
	}
}

// A release is named by its version, so release paths are readable and
// versions order by semver precedence.
func TestProtocolReleasePathsAndVersionOrder(t *testing.T) {
	calls := []string{}
	protocol, err := NewProtocol(discoveryConfig(), Discoverer{Query: fixtureQuery, Fetch: fixtureFetch(&calls)})
	if err != nil {
		t.Fatal(err)
	}
	version := "2024.3.1+B"
	operation, err := protocol.ParsePath("releases/" + version + "/windows-x64-exe-user")
	if err != nil || operation.Kind != application.ArtifactOperation || operation.Target != version {
		t.Fatal(operation, err)
	}
	for _, path := range []string{"releases/" + version + "/../file", "releases/" + version + "/file?url=x", "releases/" + version + "/https://upstream", "releases/../file", "releases/a b/file", "releases/" + strings.Repeat("9", 97) + "/file"} {
		if _, err = protocol.ParsePath(path); err == nil {
			t.Fatal("invalid path accepted", path)
		}
	}
	if order, err := protocol.CompareVersions("1.10.0", "1.9.2"); err != nil || order != 1 {
		t.Fatal("natural version order", order, err)
	}
	if _, err = protocol.CompareVersions("1.0.0", "nightly"); err == nil {
		t.Fatal("a version outside the semver form was ordered")
	}
	for _, channel := range []string{"latest", "stable"} {
		if _, err = protocol.ParsePath("releases/" + channel + "/windows-x64-exe-user"); err == nil {
			t.Fatal("a channel name accepted as a release", channel)
		}
	}
	if ch, err := protocol.CanonicalChannel("latest"); err != nil || ch != "stable" {
		t.Fatal(ch, err)
	}
	alias, aliasErr := protocol.ParsePath("download/latest/windows-x64-exe-user")
	if aliasErr != nil || alias.Kind != application.DownloadAliasOperation || alias.Target != "stable" {
		t.Fatal(alias, aliasErr)
	}
	if _, err = protocol.CanonicalChannel("preview"); err == nil {
		t.Fatal("unknown channel accepted")
	}
}
