package httpserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A file response that keeps making progress is not cut by the server's total
// write timeout; an ordinary response still is.
func TestFileResponsesOutlastTheServerWriteTimeout(t *testing.T) {
	const writeTimeout = 100 * time.Millisecond
	const chunks = 8
	slow := func(receipt bool) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if receipt {
				w = newDownloadReceipt(w, r)
			}
			for i := 0; i < chunks; i++ {
				if _, err := io.WriteString(w, "chunk"); err != nil {
					return
				}
				http.NewResponseController(w).Flush()
				time.Sleep(writeTimeout / 2) // a slow but progressing transfer, not synchronization
			}
		})
	}
	for _, receipt := range []bool{true, false} {
		server := httptest.NewUnstartedServer(slow(receipt))
		server.Config.WriteTimeout = writeTimeout
		server.Start()
		resp, err := http.Get(server.URL)
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		server.Close()
		complete := err == nil && string(body) == strings.Repeat("chunk", chunks)
		if complete != receipt {
			t.Fatalf("receipt=%v: complete=%v (%d bytes, %v)", receipt, complete, len(body), err)
		}
	}
}
