package store

import (
	"database/sql"
	"errors"
	"sync/atomic"
)

// withBeforeCommit runs hook inside every configuration transaction just
// before it commits.
func withBeforeCommit(hook func(*sql.Tx) error) option {
	return func(s *Store) { s.beforeCommit = hook }
}

// commitFault rejects configuration transactions after all of their writes
// while it is armed, so tests can check that nothing is saved or published.
type commitFault struct{ armed atomic.Bool }

func (f *commitFault) option() option {
	return withBeforeCommit(func(*sql.Tx) error {
		if f.armed.Load() {
			return errors.New("injected commit fault")
		}
		return nil
	})
}
