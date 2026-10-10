package httpserver

import (
	"context"
	"errors"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/PMExtra/RedApp/internal/store"
)

var healthOK = map[string]bool{"ok": true}

// readinessTTL is how long one readiness result answers further probes. The
// probe is anonymous, so without it every call would create a file in the
// data directory.
const readinessTTL = 5 * time.Second

// readinessCheckTimeout bounds one check; it does not depend on the request
// that happened to start it, whose cancellation must not become the cached
// result.
const readinessCheckTimeout = 5 * time.Second

// readiness caches the last readiness result; concurrent probes share one
// check.
type readiness struct {
	mu     sync.Mutex
	at     time.Time
	result *apiError     // nil means ready
	flight chan struct{} // closed when the running check has stored its result
}

func (s *Server) getLiveness(w http.ResponseWriter, r *http.Request) { writeOK(w, healthOK) }

// getReadiness reports whether SQLite answers and the data directory is
// writable; it never contacts an upstream.
func (s *Server) getReadiness(w http.ResponseWriter, r *http.Request) {
	if e := s.ready(r.Context()); e != nil {
		s.writeError(w, r, e)
		return
	}
	writeOK(w, healthOK)
}

// ready returns the cached readiness result, or runs one check shared by
// every concurrent caller when the result is older than readinessTTL.
func (s *Server) ready(ctx context.Context) *apiError {
	for {
		s.readiness.mu.Lock()
		if age := s.now().Sub(s.readiness.at); !s.readiness.at.IsZero() && age >= 0 && age < readinessTTL {
			result := s.readiness.result
			s.readiness.mu.Unlock()
			return result
		}
		if flight := s.readiness.flight; flight != nil {
			s.readiness.mu.Unlock()
			select {
			case <-flight:
				continue
			case <-ctx.Done():
				return newError(codeNotReady, ctx.Err(), "Readiness check was interrupted")
			}
		}
		flight := make(chan struct{})
		s.readiness.flight = flight
		s.readiness.mu.Unlock()
		result := s.checkReadiness()
		s.readiness.mu.Lock()
		s.readiness.at, s.readiness.result, s.readiness.flight = s.now(), result, nil
		s.readiness.mu.Unlock()
		close(flight)
		return result
	}
}

// checkReadiness pings SQLite and creates then removes a file in the data
// directory.
func (s *Server) checkReadiness() *apiError {
	ctx, cancel := context.WithTimeout(context.Background(), readinessCheckTimeout)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		return newError(codeNotReady, err, "Database is not ready")
	}
	f, err := os.CreateTemp(s.dataDir, ".health-")
	if err != nil {
		return newError(codeNotReady, err, "Data directory is not writable")
	}
	name := f.Name()
	closeErr := f.Close()
	if err = errors.Join(closeErr, os.Remove(name)); err != nil {
		return newError(codeNotReady, err, "Data directory is not writable")
	}
	return nil
}

func isNotFound(err error) bool { return errors.Is(err, store.ErrNotFound) }
