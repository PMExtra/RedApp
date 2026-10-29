package releaseconfig

import "testing"

func configuration() Config {
	return Config{DefaultChannel: "stable", RefreshSeconds: 300, Channels: map[string]Channel{"stable": {Query: ".body", Requests: []Request{{URL: "https://updates.example/api/latest?channel=stable", Context: map[string]string{"os": "windows", "arch": "x64"}}}}}, Sources: Sources{Metadata: []Source{{Origin: "https://updates.example", Prefix: "/api/"}}, Artifacts: []Source{{Origin: "https://cdn.example", Prefix: "/releases/"}}}}
}
func TestConfigurationDoesNotGrantDiscoveredSources(t *testing.T) {
	c := configuration()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	r := fixture()
	if err := c.AuthorizeArtifacts(r); err != nil {
		t.Fatal(err)
	}
	r.Artifacts[0].ChecksumURL = "https://cdn.example/releases/app.sha256"
	if c.AuthorizeArtifacts(r) == nil {
		t.Fatal("artifact boundary implicitly authorized checksum")
	}
	c.Sources.Checksums = c.Sources.Artifacts
	if err := c.AuthorizeArtifacts(r); err != nil {
		t.Fatal(err)
	}
	r.Artifacts[0].URL = "https://updates.example/api/app.exe"
	if c.AuthorizeArtifacts(r) == nil {
		t.Fatal("metadata origin implicitly authorized artifact")
	}
}
func TestConfigurationRejectsUnboundedOrAmbiguousSelection(t *testing.T) {
	for _, change := range []func(*Config){
		func(c *Config) { c.RefreshSeconds = 0 },
		func(c *Config) { c.DefaultChannel = "missing" },
		func(c *Config) { c.Channels["latest"] = c.Channels["stable"] },
		func(c *Config) {
			ch := c.Channels["stable"]
			ch.Requests = make([]Request, MaxRequests+1)
			c.Channels["stable"] = ch
		},
		func(c *Config) { c.Prewarm = []PrewarmSelection{{Channel: "stable", Keys: []string{"a", "a"}}} },
		func(c *Config) {
			c.Channels["stable"] = Channel{Query: ".", Requests: []Request{{URL: "https://evil.example/api/latest"}}}
		},
	} {
		c := configuration()
		change(&c)
		if c.Validate() == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
}

// A lookup must use the version, stays inside the metadata sources once the
// version is filled in, and never starts a new source epoch.
func TestLookupFillsTheVersionAndKeepsTheSource(t *testing.T) {
	c := configuration()
	before := c
	c.Lookup = &Channel{Query: ".body", Requests: []Request{{URL: "https://updates.example/api/versions/{version}", Context: map[string]string{"label": "v{version}"}}}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if !SameSource(&before, &c) {
		t.Fatal("a lookup started a new source epoch")
	}
	filled := c.LookupChannel("1.0+b")
	if filled.Requests[0].URL != "https://updates.example/api/versions/1.0%2Bb" || filled.Requests[0].Context["label"] != "v1.0+b" {
		t.Fatal(filled)
	}
	if c.Lookup.Requests[0].URL != "https://updates.example/api/versions/{version}" {
		t.Fatal("filling the version changed the configuration")
	}
	for _, request := range []Request{{URL: "https://updates.example/api/versions/latest"}, {URL: "https://elsewhere.example/api/{version}"}} {
		c.Lookup = &Channel{Query: ".body", Requests: []Request{request}}
		if c.Validate() == nil {
			t.Fatal("invalid lookup accepted", request.URL)
		}
	}
}
