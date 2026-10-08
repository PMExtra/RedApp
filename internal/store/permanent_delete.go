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
func (s *Store) PermanentlyDeleteApplication(key string, revision int64) error {
	if protected, err := s.canonicalApplicationProtected(key); err != nil {
		return err
	} else if protected {
		return ErrBuiltinTemplate
	}
	// Physical purge may run under Downloads.mu. Admission is fenced before
	// entering that callback, so this cleanup never acquires the publication gate.
	current, e := s.Application(key)
	if e != nil {
		return e
	}
	if current.Revision != revision {
		return ErrConflict
	}
	if current.DeletedAt == nil {
		if e = s.DeleteApplication(key, revision); e != nil {
			return e
		}
		revision++
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
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
	for _, table := range []string{"generations", "resources", "channels", "release_metadata", "app_versions", "blobs", "cleanup_previews"} {
		if _, err = tx.Exec(`DELETE FROM `+table+` WHERE app_id LIKE ?`, pattern); err != nil {
			return err
		}
	}
	for _, table := range []string{"metric_counters", "metric_samples", "metric_hours", "events", "settings"} {
		if _, err = tx.Exec(`DELETE FROM `+table+` WHERE app_id=? OR app_id LIKE ?`, prefix, pattern); err != nil {
			return err
		}
	}
	for _, table := range []string{"http_cleanup_previews", "http_cache_generations"} {
		if _, err = tx.Exec(`DELETE FROM `+table+` WHERE storage_id LIKE ?`, pattern); err != nil {
			return err
		}
	}
	for _, table := range []string{"application_instructions", "hosted_files", "download_sketches", "homepage_pins", "application_sources"} {
		if _, err = tx.Exec(`DELETE FROM `+table+` WHERE app_uid=?`, a.UID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`DELETE FROM applications WHERE uid=?`, a.UID); err != nil {
		return err
	}
	// Existing vendor order/records and globally shared icon objects are not app-owned.
	if _, err = tx.Exec(`UPDATE catalog_state SET revision=revision+1 WHERE id=1`); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) PermanentlyDeleteVendor(id string, revision int64) error {
	current, e := s.Vendor(id)
	if e != nil {
		return e
	}
	if current.Revision != revision {
		return ErrConflict
	}
	if current.DeletedAt == nil {
		if e = s.DeleteVendor(id, revision); e != nil {
			return e
		}
		revision++
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
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
	if _, err = tx.Exec(`DELETE FROM vendors WHERE uid=?`, v.UID); err != nil {
		return err
	}
	return tx.Commit()
}

var deleteObjectPath = regexp.MustCompile(`^objects/(parts/[0-9a-f]{32}\.part|http/[0-9a-f]{32}\.body|hosted/[0-9a-f]{32}|blobs/[0-9a-f]{64})$`)

func (s *Store) ProcessPendingDeletes(dir string) error {
	rows, err := s.DB.Query(`SELECT path FROM pending_object_deletes`)
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
			return errors.New("Invalid pending object deletion")
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
		if _, err = s.DB.Exec(`DELETE FROM pending_object_deletes WHERE path=?`, path); err != nil {
			return err
		}
	}
	return nil
}

// Late background observations must not recreate the private history of a deleted
// UID. Legacy static fixtures do not use this private namespace.
func PrivateApplicationExists(q directoryQuerier, app string) (bool, error) {
	if !strings.HasPrefix(app, "app/") {
		return true, nil
	}
	uid := strings.TrimPrefix(app, "app/")
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
