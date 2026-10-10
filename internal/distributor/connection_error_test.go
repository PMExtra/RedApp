package distributor

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
)

func TestConnectionFailureClassificationForSourceRetry(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
		retry bool
	}{
		{"refused", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, true},
		{"deadline", context.DeadlineExceeded, true},
		{"connection_closed", io.EOF, true},
		{"untrusted_certificate", x509.UnknownAuthorityError{}, false},
		{"cancelled_request", context.Canceled, false},
		{"other_transport_policy", errors.New("fixture transport rejected https://private.example/secret"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewPool().NewClient("http://internal.example/root", GeneralHTTP)
			if err != nil {
				t.Fatal(err)
			}
			c.HTTP.Transport = compressionTransport(func(*http.Request) (*http.Response, error) { return nil, tc.cause })
			_, err = c.Get(context.Background(), sourceURL(c, "file"), nil)
			if err == nil || errors.Is(err, ErrConnection) != tc.retry {
				t.Fatal("incorrect source retry class", err)
			}
			if strings.Contains(err.Error(), "private.example") || strings.Contains(err.Error(), "secret") {
				t.Fatal("unsafe source detail exposed", err)
			}
		})
	}
	c, _ := NewPool().NewClient("http://internal.example/root", GeneralHTTP)
	c.HTTP.Transport = compressionTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"http://another.example/file"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	if _, err := c.Get(context.Background(), sourceURL(c, "file"), nil); err == nil || errors.Is(err, ErrConnection) {
		t.Fatal("redirect policy failure classified as retryable", err)
	}
}
