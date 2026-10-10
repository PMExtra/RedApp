package distributor

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGeneralRedirectDropsResourceConditionsAndRanges(t *testing.T) {
	for _, destination := range []string{"http://source.example/files/next", "https://cdn.example/next"} {
		t.Run(destination, func(t *testing.T) {
			client, err := NewPool().NewClient("http://source.example/files", GeneralHTTP)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			client.HTTP.Transport = compressionTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return &http.Response{StatusCode: 307, Header: http.Header{"Location": {destination}}, Body: http.NoBody, Request: r}, nil
				}
				for _, key := range []string{"If-None-Match", "If-Modified-Since", "If-Match", "If-Unmodified-Since", "If-Range", "Range"} {
					if r.Header.Get(key) != "" {
						t.Errorf("%s crossed redirect", key)
					}
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("complete")), Request: r}, nil
			})
			h := http.Header{"If-None-Match": {`"etag"`}, "If-Modified-Since": {"Sat, 03 Oct 2026 00:00:00 GMT"}, "If-Match": {`"etag"`}, "If-Unmodified-Since": {"Sat, 03 Oct 2026 00:00:00 GMT"}, "If-Range": {`"etag"`}, "Range": {"bytes=1-2"}}
			resp, err := client.Get(context.Background(), sourceURL(client, "file"), h)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if calls != 2 {
				t.Fatal(calls)
			}
		})
	}
}
