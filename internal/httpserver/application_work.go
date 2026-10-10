package httpserver

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// A response lease covers body parsing, streaming, accounting and handle cleanup.
// Cancellation expires only this response's deadline (an HTTP/2 stream, not its
// shared connection). Join the callback before the ResponseWriter is released.
func (s *Server) applicationResponse(w http.ResponseWriter, r *http.Request, app string) (*http.Request, func(), error) {
	ctx, finish, err := s.store.ApplicationWork(r.Context(), app)
	if err != nil {
		return nil, nil, err
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(done)
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now())
		_ = http.NewResponseController(w).SetReadDeadline(time.Now())
		if r.Body != nil {
			r.Body.Close()
		}
	})
	return r.WithContext(ctx), func() {
		if !stop() {
			<-done
		}
		finish()
	}, nil
}

var errDeletePending = errors.New("application deletion is not complete")

func (s *Server) deleteApplication(ctx context.Context, key, expectedUID string, revision int64) (err error) {
	uid, drained, err := s.store.PrepareGuardedApplicationDeletion(key, expectedUID, revision)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(errDeletePending, err)
		}
	}()
	wait := s.deleteWait
	if wait == 0 {
		wait = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	select {
	case <-drained:
	case <-ctx.Done():
		return ctx.Err()
	}
	var purge func(remove func() error) error
	if s.downloads != nil {
		purge = func(remove func() error) error { return s.downloads.PurgeApplication(uid, remove) }
	}
	return s.store.FinishApplicationDeletion(uid, purge)
}
