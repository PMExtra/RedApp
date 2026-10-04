package distributor

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type compressionTransport func(*http.Request) (*http.Response, error)

func (f compressionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRejectUnexpectedOrTransparentDecompression(t *testing.T) {
	for _, tc := range []struct {
		encoding string
		decoded  bool
	}{{"gzip", false}, {"", true}} {
		c, err := New("https://upstream.example/artifacts")
		if err != nil {
			t.Fatal(err)
		}
		c.HTTP.Transport = compressionTransport(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Accept-Encoding") != "identity" || r.Header.Get("Range") != "bytes=3-" {
				t.Fatal("resume representation changed")
			}
			return &http.Response{StatusCode: 206, Header: http.Header{"Content-Encoding": []string{tc.encoding}}, Uncompressed: tc.decoded, Body: io.NopCloser(strings.NewReader("payload"))}, nil
		})
		if _, err = c.Get(context.Background(), c.URL("asset"), http.Header{"Range": []string{"bytes=3-"}}); err == nil {
			t.Fatal("unsafe representation accepted", tc)
		}
	}
}

func TestRejectRepeatedContentEncoding(t *testing.T) {
	c, _ := New("https://upstream.example/artifacts")
	c.HTTP.Transport = compressionTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Encoding": {"identity", "gzip"}}, Body: io.NopCloser(strings.NewReader("payload"))}, nil
	})
	if _, err := c.Get(context.Background(), c.URL("asset"), nil); err == nil {
		t.Fatal("repeated unsafe encoding accepted")
	}
}
