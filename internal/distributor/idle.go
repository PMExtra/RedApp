package distributor

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// DefaultIdleTimeout bounds how long one response body read may wait without
// receiving bytes. Response bodies have no overall deadline: large artifacts
// stream for as long as the upstream keeps making progress. Dial, TLS and
// response-header phases keep their own transport timeouts.
const DefaultIdleTimeout = 60 * time.Second

// ErrIdleTimeout reports an upstream body that stopped delivering bytes.
var ErrIdleTimeout error = &RequestError{Kind: KindTimeout, retryable: true, message: "Upstream read idle timeout"}

// idleBody cancels its request when a single Read waits longer than timeout.
// Time spent by the caller between reads (e.g. a slow downstream client) is
// not counted, so only a stalled upstream trips it.
type idleBody struct {
	io.ReadCloser
	timeout time.Duration
	timer   *time.Timer
	cancel  context.CancelFunc
	expired atomic.Bool
}

// watchBody takes ownership of cancel, which must cancel resp's request context.
func watchBody(resp *http.Response, timeout time.Duration, cancel context.CancelFunc) {
	b := &idleBody{ReadCloser: resp.Body, timeout: timeout, cancel: cancel}
	b.timer = time.AfterFunc(timeout, func() { b.expired.Store(true); cancel() })
	b.timer.Stop()
	resp.Body = b
}

func (b *idleBody) Read(p []byte) (int, error) {
	if b.expired.Load() {
		return 0, ErrIdleTimeout
	}
	b.timer.Reset(b.timeout)
	n, err := b.ReadCloser.Read(p)
	b.timer.Stop()
	if err != nil && err != io.EOF && b.expired.Load() {
		return n, ErrIdleTimeout
	}
	return n, err
}

func (b *idleBody) Close() error {
	b.timer.Stop()
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}
