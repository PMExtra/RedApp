package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/PMExtra/RedApp/internal/identity"
)

// Application work is registered before it can open files, start upstream work,
// or expose a background worker. Deletion closes admission before cancellation.
// A lease ends only after all its handles and final writes have been released.
type applicationWork struct {
	mu   sync.Mutex
	apps map[string]*appWork
}
type appWork struct {
	blocked bool
	next    uint64
	tasks   map[uint64]context.CancelFunc
	drained chan struct{}
	closed  bool
}

func (w *applicationWork) app(uid string) *appWork {
	if w.apps == nil {
		w.apps = make(map[string]*appWork)
	}
	a := w.apps[uid]
	if a == nil {
		a = &appWork{tasks: make(map[uint64]context.CancelFunc), drained: make(chan struct{})}
		w.apps[uid] = a
	}
	return a
}
func (a *appWork) signal() {
	if a.blocked && len(a.tasks) == 0 && !a.closed {
		close(a.drained)
		a.closed = true
	}
}

// ApplicationWork accepts a stable UID or a source storage ID. Static provider
// fixtures have no dynamic directory identity and retain their existing lifetime.
func (s *Store) ApplicationWork(ctx context.Context, app string) (context.Context, func(), error) {
	uid := app
	if parsed, _, ok := identity.ParseStorageID(app); ok {
		uid = parsed
	}
	if !identity.ValidUID(uid) {
		if !strings.HasPrefix(app, "app/") && identity.ValidKey(app) {
			return ctx, func() {}, nil
		}
		return nil, nil, ErrInvalidDirectory
	}
	s.work.mu.Lock()
	defer s.work.mu.Unlock()
	a := s.work.app(uid)
	if a.blocked {
		return nil, nil, ErrSourceInactive
	}
	var live bool
	if err := s.DB.QueryRow(`SELECT 1 FROM applications WHERE uid=?`, uid).Scan(&live); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrSourceInactive
		}
		return nil, nil, err
	}
	if !live {
		return nil, nil, ErrSourceInactive
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	a.next++
	id := a.next
	a.tasks[id] = cancel
	var once sync.Once
	return ctx, func() {
		once.Do(func() { cancel(); s.work.mu.Lock(); delete(a.tasks, id); a.signal(); s.work.mu.Unlock() })
	}, nil
}

// PrepareApplicationDeletion validates everything before interrupting any task.
// The durable intent distinguishes force deletion from historical soft deletes.
func (s *Store) PrepareApplicationDeletion(key string, revision int64) (string, <-chan struct{}, error) {
	if _, ok := BuiltinApplicationTemplate(key); ok {
		return "", nil, ErrBuiltinTemplate
	}
	s.work.mu.Lock()
	defer s.work.mu.Unlock()
	tx, err := s.DB.Begin()
	if err != nil {
		return "", nil, err
	}
	defer tx.Rollback()
	app, err := readApplication(tx, key)
	if err != nil {
		return "", nil, err
	}
	var original int64
	err = tx.QueryRow(`SELECT requested_revision FROM pending_application_deletes WHERE app_uid=?`, app.UID).Scan(&original)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", nil, err
	}
	pending := err == nil
	if revision != app.Revision && (!pending || revision != original) {
		return "", nil, ErrConflict
	}
	if !pending {
		if _, err = tx.Exec(`INSERT INTO pending_application_deletes(app_uid,requested_revision) VALUES(?,?)`, app.UID, revision); err != nil {
			return "", nil, err
		}
		if _, err = tx.Exec(`UPDATE applications SET enabled=0,revision=revision+1,deleted_at_s=? WHERE uid=?`, time.Now().UTC().Unix(), app.UID); err != nil {
			return "", nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return "", nil, err
	}
	a := s.work.app(app.UID)
	a.blocked = true
	for _, cancel := range a.tasks {
		cancel()
	}
	a.signal()
	return app.UID, a.drained, nil
}

func (s *Store) FinishApplicationDeletion(uid string) error {
	var key string
	var revision int64
	err := s.DB.QueryRow(`SELECT v.id||'/'||a.id,a.revision FROM pending_application_deletes p JOIN applications a ON a.uid=p.app_uid JOIN vendors v ON v.uid=a.vendor_uid WHERE a.uid=?`, uid).Scan(&key, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.PermanentlyDeleteApplication(key, revision)
}

// Reload admission tombstones before any service can use a reopened store.
func (s *Store) loadApplicationDeletionGates() error {
	rows, err := s.DB.Query(`SELECT app_uid FROM pending_application_deletes`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var uid string
		if err = rows.Scan(&uid); err != nil {
			return err
		}
		a := s.work.app(uid)
		a.blocked = true
		a.signal()
	}
	return rows.Err()
}

// Called before runtime services start. No previous process can retain a lease
// under the exclusive data-directory lock. Unrelated soft deletes stay intact.
func (s *Store) RecoverApplicationDeletions() error {
	rows, err := s.DB.Query(`SELECT app_uid FROM pending_application_deletes`)
	if err != nil {
		return err
	}
	var uids []string
	for rows.Next() {
		var uid string
		if err = rows.Scan(&uid); err != nil {
			break
		}
		uids = append(uids, uid)
	}
	readErr := rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if readErr != nil {
		return readErr
	}
	for _, uid := range uids {
		if err = s.FinishApplicationDeletion(uid); err != nil {
			return err
		}
	}
	return nil
}
