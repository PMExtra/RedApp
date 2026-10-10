package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"github.com/PMExtra/RedApp/internal/identity"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func queueDelete(tx *sql.Tx, path string) error {
	_, err := tx.Exec(`INSERT OR IGNORE INTO pending_object_deletes(path) VALUES(?)`, path)
	return err
}

// PermanentlyDeleteApplication soft-deletes (if needed) and purges the
// application's rows in one transaction.
func (s *Store) PermanentlyDeleteApplication(key string, revision int64) error {
	if protected, err := s.canonicalApplicationProtected(key); err != nil {
		return err
	} else if protected {
		return ErrBuiltinTemplate
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.permanentlyDeleteApplicationLocked(key, revision, true)
}

// permanentlyDeleteApplicationLocked requires writeMu. Purging an application
// that is already soft-deleted changes no published configuration, so it never
// prepares a publication; that path may therefore run under Downloads.mu.
// Without allowPublish a live application is a conflict rather than a publication.
func (s *Store) permanentlyDeleteApplicationLocked(key string, revision int64, allowPublish bool) error {
	// Persist buffered counters first so global totals keep the application's final traffic.
	s.SettleCounters()
	current, e := s.Application(key)
	if e != nil {
		return e
	}
	if current.Revision != revision {
		return ErrConflict
	}
	if current.DeletedAt == nil {
		if !allowPublish {
			return ErrConflict
		}
		return s.writeConfigurationLocked(func(w *configSet) error {
			a, err := w.softDeleteApplication(key, revision)
			if err == nil {
				w.removedApps = append(w.removedApps, a.UID)
			}
			return err
		}, func(tx *sql.Tx) error { return purgeApplication(tx, key, revision+1) })
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = purgeApplication(tx, key, revision); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.dropFromView([]string{current.UID}, nil)
	return nil
}
func purgeApplication(tx *sql.Tx, key string, revision int64) error {
	a, err := readApplication(tx, key)
	if err != nil {
		return err
	}
	if revision != a.Revision {
		return ErrConflict
	}
	prefix := identity.MetricsID(a.UID)
	pattern := prefix + "-e%"
	// Save only generated private object paths before dropping their relational owners.
	for _, q := range []struct {
		query, dir, suffix string
		arg                any
	}{
		{`SELECT id FROM generations WHERE app_id LIKE ?`, "objects/parts/", ".part", pattern},
		{`SELECT id FROM http_cache_generations WHERE storage_id LIKE ?`, "objects/http/", ".body", pattern},
		{`SELECT id FROM hosted_files WHERE app_uid=?`, "objects/hosted/", "", a.UID},
	} {
		rows, e := tx.Query(q.query, q.arg)
		if e != nil {
			return e
		}
		paths := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				break
			}
			if !identity.ValidUID(id) {
				e = ErrInvalidDirectory
				break
			}
			paths = append(paths, q.dir+id+q.suffix)
		}
		rowErr := rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if rowErr != nil {
			return rowErr
		}
		for _, path := range paths {
			if err = queueDelete(tx, path); err != nil {
				return err
			}
		}
	}
	rows, err := tx.Query(`SELECT epoch FROM application_sources WHERE app_uid=?`, a.UID)
	if err != nil {
		return err
	}
	paths := []string{}
	for rows.Next() {
		var epoch int64
		if err = rows.Scan(&epoch); err != nil {
			break
		}
		hash := sha256.Sum256([]byte(identity.StorageID(a.UID, epoch)))
		paths = append(paths, "objects/blobs/"+hex.EncodeToString(hash[:]))
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if rowErr != nil {
		return rowErr
	}
	for _, path := range paths {
		if err = queueDelete(tx, path); err != nil {
			return err
		}
	}
	// Rows keyed by the storage or metrics namespace have no foreign key to the
	// application. Deleting a version cascades to its metadata, channels,
	// resources and generations, which must go before the blobs they reference.
	for _, statement := range []string{
		`DELETE FROM app_versions WHERE app_id LIKE ?`,
		`DELETE FROM blobs WHERE app_id LIKE ?`,
		`DELETE FROM http_cache_generations WHERE storage_id LIKE ?`,
	} {
		if _, err = tx.Exec(statement, pattern); err != nil {
			return err
		}
	}
	for _, table := range []string{"metric_counters", "metric_samples", "metric_hours", "events"} {
		if _, err = tx.Exec(`DELETE FROM `+table+` WHERE app_id=? OR app_id LIKE ?`, prefix, pattern); err != nil {
			return err
		}
	}
	// Every UID-keyed row (sources, configuration, notes, taxonomy, hosted files,
	// ranking, prewarm, retention, previews) cascades from the application.
	if _, err = tx.Exec(`DELETE FROM applications WHERE uid=?`, a.UID); err != nil {
		return err
	}
	// Existing vendor order/records and globally shared icon objects are not app-owned.
	_, err = tx.Exec(`UPDATE catalog_state SET revision=revision+1 WHERE id=1`)
	return err
}

// PermanentlyDeleteVendor soft-deletes (if needed) and removes the vendor in one
// transaction.
func (s *Store) PermanentlyDeleteVendor(id string, revision int64) error {
	if _, ok := BuiltinVendorTemplate(id); ok {
		return ErrBuiltinTemplate
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	current, e := s.Vendor(id)
	if e != nil {
		return e
	}
	if current.Revision != revision {
		return ErrConflict
	}
	if current.DeletedAt == nil {
		return s.writeConfigurationLocked(func(w *configSet) error {
			v, err := w.softDeleteVendor(id, revision)
			if err == nil {
				w.removedVendors = append(w.removedVendors, v.UID)
			}
			return err
		}, func(tx *sql.Tx) error { return purgeVendor(tx, id, revision+1) })
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = purgeVendor(tx, id, revision); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.dropFromView(nil, []string{current.UID})
	return nil
}
func purgeVendor(tx *sql.Tx, id string, revision int64) error {
	v, err := readVendor(tx, id)
	if err != nil {
		return err
	}
	if v.Revision != revision {
		return ErrConflict
	}
	var count int
	if err = tx.QueryRow(`SELECT count(*) FROM applications WHERE vendor_uid=?`, v.UID).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return ErrVendorHasApplications
	}
	_, err = tx.Exec(`DELETE FROM vendors WHERE uid=?`, v.UID)
	return err
}

var deleteObjectPath = regexp.MustCompile(`^objects/(parts/[0-9a-f]{32}\.part|http/[0-9a-f]{32}\.body|hosted/[0-9a-f]{32}|blobs/[0-9a-f]{64})$`)

func (s *Store) ProcessPendingDeletes(dir string) error {
	rows, err := s.read.Query(`SELECT path FROM pending_object_deletes`)
	if err != nil {
		return err
	}
	paths := []string{}
	for rows.Next() {
		var path string
		if err = rows.Scan(&path); err != nil {
			break
		}
		paths = append(paths, path)
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if rowErr != nil {
		return rowErr
	}
	if len(paths) == 0 {
		return nil
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, path := range paths {
		if !deleteObjectPath.MatchString(path) {
			return errors.New("invalid pending object deletion")
		}
		if strings.HasPrefix(path, "objects/blobs/") {
			err = root.RemoveAll(path)
		} else {
			err = root.Remove(path)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent, e := root.Open(filepath.Dir(path))
		if e == nil {
			e = parent.Sync()
			parent.Close()
		}
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
		if _, err = s.db.Exec(`DELETE FROM pending_object_deletes WHERE path=?`, path); err != nil {
			return err
		}
	}
	return nil
}

// applicationNamespaceExists reports whether the application owning a metrics
// or storage namespace (app/<uid> or app/<uid>-e<epoch>) still exists, so late
// background observations cannot recreate the history of a deleted UID.
func applicationNamespaceExists(q directoryQuerier, app string) (bool, error) {
	uid, ok := strings.CutPrefix(app, "app/")
	if !ok {
		return false, ErrInvalidDirectory
	}
	if i := strings.Index(uid, "-e"); i >= 0 {
		uid = uid[:i]
	}
	if !identity.ValidUID(uid) {
		return false, ErrInvalidDirectory
	}
	var count int
	err := q.QueryRow(`SELECT count(*) FROM applications WHERE uid=?`, uid).Scan(&count)
	return count != 0, err
}
