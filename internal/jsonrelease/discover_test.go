package jsonrelease

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/PMExtra/RedApp/internal/releaseconfig"
)

// Synthetic fixtures exercise the engine contract, not jq compatibility or
// currently unreachable official vendor endpoints/checksum files.
func discoveryConfig() releaseconfig.Config {
	return releaseconfig.Config{DefaultChannel: "stable", RefreshSeconds: 300, Channels: map[string]releaseconfig.Channel{"stable": {Query: "fixture-transform", Requests: []releaseconfig.Request{{URL: "https://updates.example/api/windows", Context: map[string]string{"os": "windows", "arch": "x64", "key": "windows-x64-exe-user"}}, {URL: "https://updates.example/api/linux", Context: map[string]string{"os": "linux", "arch": "x64", "key": "linux-x64-tar"}}}}}, Sources: releaseconfig.Sources{Metadata: []releaseconfig.Source{{Origin: "https://updates.example", Prefix: "/api/"}}, Artifacts: []releaseconfig.Source{{Origin: "https://cdn.example", Prefix: "/files/"}}, Checksums: []releaseconfig.Source{{Origin: "https://cdn.example", Prefix: "/checksums/"}}}}
}
func response(raw, body string) *http.Response {
	request, _ := http.NewRequest(http.MethodGet, raw, nil)
	return &http.Response{StatusCode: 200, Header: http.Header{}, Request: request, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}
}
func fixtureQuery(_ context.Context, _ string, input json.RawMessage) (json.RawMessage, error) {
	var wrapper struct {
		Body struct {
			Version string `json:"version"`
			Build   string `json:"build"`
		}
		Context map[string]string
	}
	if err := json.Unmarshal(input, &wrapper); err != nil {
		return nil, err
	}
	r := releaseFixture()
	r.Version = wrapper.Body.Version
	r.Build = wrapper.Body.Build
	a := &r.Artifacts[0]
	a.OS = wrapper.Context["os"]
	a.Arch = wrapper.Context["arch"]
	a.Key = wrapper.Context["key"]
	a.SHA256 = ""
	a.ChecksumURL = "https://cdn.example/checksums/" + a.Key
	a.ChecksumFilename = a.Key
	return json.Marshal(r)
}
func fixtureFetch(calls *[]string) FetchFunc {
	return func(_ context.Context, raw string, allow func(string) bool) (*http.Response, error) {
		*calls = append(*calls, raw)
		if !allow(raw) {
			return nil, ErrSource
		}
		if strings.HasPrefix(raw, "https://updates.example/api/") {
			return response(raw, `{"version":"1.2.3","build":"abc"}`), nil
		}
		if strings.HasPrefix(raw, "https://cdn.example/checksums/") {
			return response(raw, strings.Repeat("a", 64)+"  "+strings.TrimPrefix(raw, "https://cdn.example/checksums/")), nil
		}
		return nil, errors.New("unexpected artifact body request")
	}
}
func TestPreviewFetchesOnlyMetadataAndMatchingSidecars(t *testing.T) {
	calls := []string{}
	d := Discoverer{Query: fixtureQuery, Fetch: fixtureFetch(&calls)}
	r, err := d.Discover(context.Background(), discoveryConfig(), "latest")
	if err != nil || len(r.Artifacts) != 2 || len(calls) != 4 {
		t.Fatal(r, calls, err)
	}
	for _, a := range r.Artifacts {
		if a.SHA256 != strings.Repeat("a", 64) {
			t.Fatal(a)
		}
	}
	if r.Artifacts[0].OS != "windows" || r.Artifacts[1].OS != "linux" {
		t.Fatal("matrix context lost", r)
	}
	if _, err = d.Discover(context.Background(), discoveryConfig(), "preview"); err == nil {
		t.Fatal("undeclared channel accepted")
	}
}
func TestDiscoveryRejectsMatrixVersionMismatchAndUnauthorizedOutput(t *testing.T) {
	for _, change := range []func(*releaseconfig.Release){func(r *releaseconfig.Release) { r.Build = "different" }, func(r *releaseconfig.Release) { r.Artifacts[0].URL = "https://evil.example/package" }} {
		calls := []string{}
		count := 0
		d := Discoverer{Fetch: fixtureFetch(&calls), Query: func(ctx context.Context, query string, input json.RawMessage) (json.RawMessage, error) {
			raw, err := fixtureQuery(ctx, query, input)
			if err != nil {
				return nil, err
			}
			count++
			var r releaseconfig.Release
			if err = json.Unmarshal(raw, &r); err != nil {
				return nil, err
			}
			if count == 2 {
				change(&r)
			}
			return json.Marshal(r)
		}}
		if _, err := d.Discover(context.Background(), discoveryConfig(), "stable"); err == nil {
			t.Fatal("invalid snapshot accepted")
		}
		if len(calls) != 2 {
			t.Fatal("sidecars fetched before matrix validation", calls)
		}
	}
}
func TestDiscoveryCancellationAndLimits(t *testing.T) {
	calls := []string{}
	c := discoveryConfig()
	d := Discoverer{Query: fixtureQuery, Fetch: fixtureFetch(&calls)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.Discover(ctx, c, "stable"); !errors.Is(err, context.Canceled) || len(calls) != 0 {
		t.Fatal(err, calls)
	}
	d.Fetch = func(_ context.Context, raw string, _ func(string) bool) (*http.Response, error) {
		r := response(raw, "")
		r.ContentLength = releaseconfig.MaxMetadataBytes + 1
		return r, nil
	}
	if _, err := d.Discover(context.Background(), c, "stable"); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	d.Fetch = fixtureFetch(&calls)
	d.Query = func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(strings.Repeat(" ", releaseconfig.MaxOutputBytes+1)), nil
	}
	if _, err := d.Discover(context.Background(), c, "stable"); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	d.Query = func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
		cancel()
		return json.Marshal(releaseFixture())
	}
	if _, err := d.Discover(ctx, c, "stable"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestStrictOutputAndNetworkResponseBoundaries(t *testing.T) {
	raw, _ := json.Marshal(releaseFixture())
	for _, bad := range []string{`{"version":"1","Version":"2","artifacts":[]}`, string(raw) + string(raw), strings.Replace(string(raw), `"version":`, `"unexpected":true,"version":`, 1)} {
		if _, err := DecodeRelease([]byte(bad), false); err == nil {
			t.Fatal("ambiguous output accepted")
		}
	}
	c := discoveryConfig()
	for _, modify := range []func(*http.Response){func(r *http.Response) { r.Request.URL.Host = "evil.example" }, func(r *http.Response) { r.ContentLength++ }, func(r *http.Response) { r.StatusCode = 500 }, func(r *http.Response) { r.Header.Set("Content-Encoding", "gzip") }} {
		d := Discoverer{Query: fixtureQuery, Fetch: func(_ context.Context, raw string, allow func(string) bool) (*http.Response, error) {
			if allow("https://evil.example/api/latest") {
				t.Fatal("redirect callback permits unrelated origin")
			}
			r := response(raw, `{"version":"1.2.3","build":"abc"}`)
			modify(r)
			return r, nil
		}}
		if _, err := d.Discover(context.Background(), c, "stable"); err == nil {
			t.Fatal("invalid response accepted")
		}
	}
}

func TestSidecarMismatchAndUnknownLengthAreRejected(t *testing.T) {
	calls := []string{}
	base := fixtureFetch(&calls)
	d := Discoverer{Query: fixtureQuery, Fetch: func(ctx context.Context, raw string, allow func(string) bool) (*http.Response, error) {
		r, err := base(ctx, raw, allow)
		if err == nil && strings.Contains(raw, "/checksums/") {
			r.Body.Close()
			return response(raw, strings.Repeat("a", 64)+"  unrelated.exe"), nil
		}
		return r, err
	}}
	if _, err := d.Discover(context.Background(), discoveryConfig(), "stable"); err == nil {
		t.Fatal("unmatched checksum sidecar accepted")
	}
	d.Fetch = func(_ context.Context, raw string, _ func(string) bool) (*http.Response, error) {
		r := response(raw, strings.Repeat(" ", releaseconfig.MaxMetadataBytes+1))
		r.ContentLength = -1
		return r, nil
	}
	if _, err := d.Discover(context.Background(), discoveryConfig(), "stable"); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	d.Fetch = base
	d.Query = func(ctx context.Context, query string, raw json.RawMessage) (json.RawMessage, error) {
		result, err := fixtureQuery(ctx, query, raw)
		if err != nil {
			return nil, err
		}
		var r releaseconfig.Release
		if err = json.Unmarshal(result, &r); err != nil {
			return nil, err
		}
		r.Artifacts[0].SHA256 = strings.Repeat("b", 64)
		return json.Marshal(r)
	}
	if _, err := d.Discover(context.Background(), discoveryConfig(), "stable"); err == nil {
		t.Fatal("metadata/sidecar disagreement accepted")
	}
}
