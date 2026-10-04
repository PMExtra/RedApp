package store

import (
	"database/sql"
	"errors"
	"github.com/PMExtra/RedApp/internal/identity"
	"github.com/PMExtra/RedApp/internal/pathmatch"
	"time"
)

type HostedFile struct {
	AppUID    string    `json:"-"`
	Path      string    `json:"path"`
	ID        string    `json:"id"`
	SHA256    string    `json:"sha256"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

const hostedColumns = `app_uid,path,id,sha256,size_bytes,created_at_s`

func scanHosted(row scanner) (HostedFile, error) {
	var f HostedFile
	var at int64
	err := row.Scan(&f.AppUID, &f.Path, &f.ID, &f.SHA256, &f.SizeBytes, &at)
	f.CreatedAt = time.Unix(at, 0).UTC()
	return f, err
}
func ValidHostedPath(path string) bool {
	return len(path) > 0 && len(path) <= 4096 && path[0] != '/' && path[len(path)-1] != '/' && pathmatch.ValidatePath("/"+path) == nil
}
func (s *Store) HostedFile(uid, path string) (HostedFile, error) {
	return scanHosted(s.DB.QueryRow(`SELECT `+hostedColumns+` FROM hosted_files WHERE app_uid=? AND path=?`, uid, path))
}
func (s *Store) HostedPage(uid string, page, limit int) (Page[HostedFile], error) {
	if !identity.ValidUID(uid) || page < 1 || page > 1000000000 || limit < 1 || limit > 100 {
		return Page[HostedFile]{}, ErrInvalidDirectory
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return Page[HostedFile]{}, err
	}
	defer tx.Rollback()
	var total int64
	if err = tx.QueryRow(`SELECT COUNT(*) FROM hosted_files WHERE app_uid=?`, uid).Scan(&total); err != nil {
		return Page[HostedFile]{}, err
	}
	result := NewPage[HostedFile](page, limit, total)
	rows, err := tx.Query(`SELECT `+hostedColumns+` FROM hosted_files WHERE app_uid=? ORDER BY path LIMIT ? OFFSET ?`, uid, limit, (result.Page-1)*limit)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		f, e := scanHosted(rows)
		if e != nil {
			rows.Close()
			return result, e
		}
		result.Items = append(result.Items, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}
func (s *Store) CommitHosted(key string, appRevision, vendorRevision int64, expected string, file HostedFile) (HostedFile, error) {
	if !identity.ValidUID(file.AppUID) || !identity.ValidUID(file.ID) || !ValidHostedPath(file.Path) || len(file.SHA256) != 64 || file.SizeBytes < 0 {
		return HostedFile{}, ErrInvalidDirectory
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return HostedFile{}, err
	}
	defer tx.Rollback()
	app, err := readApplication(tx, key)
	if err != nil {
		return HostedFile{}, err
	}
	vendor, err := readVendor(tx, app.VendorID)
	if err != nil {
		return HostedFile{}, err
	}
	if app.Provider != "hosted" || app.UID != file.AppUID || app.DeletedAt != nil || vendor.DeletedAt != nil || app.Revision != appRevision || vendor.Revision != vendorRevision {
		return HostedFile{}, ErrConflict
	}
	old, err := scanHosted(tx.QueryRow(`SELECT `+hostedColumns+` FROM hosted_files WHERE app_uid=? AND path=?`, file.AppUID, file.Path))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return old, err
	}
	if old.ID != expected {
		return old, ErrConflict
	}
	_, err = tx.Exec(`INSERT INTO hosted_files(app_uid,path,id,sha256,size_bytes,created_at_s) VALUES(?,?,?,?,?,?) ON CONFLICT(app_uid,path) DO UPDATE SET id=excluded.id,sha256=excluded.sha256,size_bytes=excluded.size_bytes,created_at_s=excluded.created_at_s`, file.AppUID, file.Path, file.ID, file.SHA256, file.SizeBytes, file.CreatedAt.Unix())
	if err != nil {
		return old, err
	}
	return old, tx.Commit()
}
func (s *Store) DeleteHosted(uid, id string) (HostedFile, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return HostedFile{}, err
	}
	defer tx.Rollback()
	f, err := scanHosted(tx.QueryRow(`SELECT `+hostedColumns+` FROM hosted_files WHERE app_uid=? AND id=?`, uid, id))
	if err != nil {
		return f, err
	}
	_, err = tx.Exec(`DELETE FROM hosted_files WHERE app_uid=? AND id=?`, uid, id)
	if err != nil {
		return f, err
	}
	return f, tx.Commit()
}
func (s *Store) HostedIDs() (map[string]bool, error) {
	rows, err := s.DB.Query(`SELECT id FROM hosted_files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
