package store

import (
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
)

// setBeforeCommit runs hook inside every configuration transaction just
// before it commits, until the test ends.
func setBeforeCommit(t *testing.T, hook func(*sql.Tx) error) {
	t.Helper()
	beforeCommit = hook
	t.Cleanup(func() { beforeCommit = nil })
}

// commitFault rejects configuration transactions after all of their writes
// while it is armed, so tests can check that nothing is saved or published.
type commitFault struct{ armed atomic.Bool }

// injectCommitFault installs an unarmed commitFault for the rest of the test.
func injectCommitFault(t *testing.T) *commitFault {
	t.Helper()
	f := &commitFault{}
	setBeforeCommit(t, func(*sql.Tx) error {
		if f.armed.Load() {
			return errors.New("injected commit fault")
		}
		return nil
	})
	return f
}
