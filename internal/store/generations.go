package store

import (
	"database/sql"
	"errors"
	"time"
)

type Generation struct {
	DownloadNS                                                         int64
	ID, AppID, Version, ResourceKey, ExpectedSHA256, BlobSHA256, Phase string
	IsCurrent                                                          bool
	RetiredAt                                                          *time.Time
	Bytes, SourceBytes                                                 int64
	TotalBytes                                                         *int64
	ETag                                                               string
	Resumes                                                            int
	StartedAt                                                          time.Time
	FinishedAt                                                         *time.Time
	VerificationNS                                                     *int64
	LastErrorCode                                                      string
	FullRetry                                                          bool
}
type Blob struct {
	AppID, SHA256 string
	SizeBytes     int64
	VerifiedAt    time.Time
}

const generationColumns = "id,app_id,version,resource_key,expected_sha256,blob_sha256,phase,is_current,retired_at_s,bytes,total_bytes,source_bytes,etag,resumes,started_at_s,finished_at_s,verification_ns,last_error_code,full_retry,download_ns"

func generationArgs(g Generation) []any {
	var blob any
	if g.BlobSHA256 != "" {
		blob = g.BlobSHA256
	}
	return []any{g.ID, g.AppID, g.Version, g.ResourceKey, g.ExpectedSHA256, blob, g.Phase, g.IsCurrent, unixPointer(g.RetiredAt), g.Bytes, g.TotalBytes, g.SourceBytes, g.ETag, g.Resumes, g.StartedAt.Unix(), unixPointer(g.FinishedAt), g.VerificationNS, g.LastErrorCode, g.FullRetry, g.DownloadNS}
}

type scanner interface{ Scan(...any) error }

func scanGeneration(row scanner) (Generation, error) {
	var g Generation
	var blob, etag, last sql.NullString
	var retired, finished sql.NullInt64
	var started int64
	err := row.Scan(&g.ID, &g.AppID, &g.Version, &g.ResourceKey, &g.ExpectedSHA256, &blob, &g.Phase, &g.IsCurrent, &retired, &g.Bytes, &g.TotalBytes, &g.SourceBytes, &etag, &g.Resumes, &started, &finished, &g.VerificationNS, &last, &g.FullRetry, &g.DownloadNS)
	g.BlobSHA256 = blob.String
	g.ETag = etag.String
	g.LastErrorCode = last.String
	g.StartedAt = time.Unix(started, 0).UTC()
	g.RetiredAt = timePointer(retired)
	g.FinishedAt = timePointer(finished)
	return g, err
}
func (s *Store) Generations() ([]Generation, error) {
	rows, err := s.DB.Query("SELECT " + generationColumns + " FROM generations ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Generation{}
	for rows.Next() {
		g, err := scanGeneration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
func validGeneration(g Generation) bool {
	return ValidAppID(g.AppID) && g.ID != "" && g.Version != "" && g.ResourceKey != "" && !g.StartedAt.IsZero() && g.Bytes >= 0 && g.SourceBytes >= 0 && g.Resumes >= 0 && g.DownloadNS >= 0
}
func (s *Store) CreateGeneration(g Generation) error {
	if !validGeneration(g) || !g.IsCurrent || g.RetiredAt != nil {
		return errors.New("Invalid new generation")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE generations SET is_current=0,retired_at_s=? WHERE app_id=? AND version=? AND resource_key=? AND is_current=1", time.Now().Unix(), g.AppID, g.Version, g.ResourceKey); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO generations("+generationColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", generationArgs(g)...); err != nil {
		return err
	}
	return tx.Commit()
}

// SaveGeneration checkpoints progress without changing ownership, current status
// or retirement. A late writer therefore cannot restore a replaced head.
func (s *Store) SaveGeneration(g Generation) error {
	if !validGeneration(g) {
		return errors.New("Invalid generation checkpoint")
	}
	var blob any
	if g.BlobSHA256 != "" {
		blob = g.BlobSHA256
	}
	result, err := s.DB.Exec(`UPDATE generations SET blob_sha256=?,phase=?,bytes=?,total_bytes=?,source_bytes=?,etag=?,resumes=?,finished_at_s=?,verification_ns=?,last_error_code=?,full_retry=?,download_ns=? WHERE id=? AND app_id=? AND version=? AND resource_key=? AND expected_sha256=?`, blob, g.Phase, g.Bytes, g.TotalBytes, g.SourceBytes, g.ETag, g.Resumes, unixPointer(g.FinishedAt), g.VerificationNS, g.LastErrorCode, g.FullRetry, g.DownloadNS, g.ID, g.AppID, g.Version, g.ResourceKey, g.ExpectedSHA256)
	return affected(result, err)
}
func affected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return sql.ErrNoRows
	}
	return err
}
func (s *Store) RetireGeneration(app, id string, at time.Time) error {
	if err := requireApp(app); err != nil {
		return err
	}
	result, err := s.DB.Exec("UPDATE generations SET is_current=0,retired_at_s=COALESCE(retired_at_s,?) WHERE id=? AND app_id=?", at.Unix(), id, app)
	return affected(result, err)
}
func (s *Store) DeleteGeneration(app, id string) error {
	if err := requireApp(app); err != nil {
		return err
	}
	_, err := s.DB.Exec("DELETE FROM generations WHERE id=? AND app_id=? AND is_current=0", id, app)
	return err
}
func putBlob(tx *sql.Tx, b Blob) error {
	if requireApp(b.AppID) != nil || b.SizeBytes < 0 || b.VerifiedAt.IsZero() {
		return errors.New("Invalid verified blob")
	}
	var size int64
	err := tx.QueryRow("SELECT size_bytes FROM blobs WHERE app_id=? AND sha256=?", b.AppID, b.SHA256).Scan(&size)
	if err == nil && size != b.SizeBytes {
		return errors.New("Verified blob size changed")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.Exec("INSERT INTO blobs VALUES(?,?,?,?) ON CONFLICT(app_id,sha256) DO UPDATE SET verified_at_s=excluded.verified_at_s", b.AppID, b.SHA256, b.SizeBytes, b.VerifiedAt.Unix())
	return err
}
func (s *Store) PutBlob(b Blob) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = putBlob(tx, b); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Blob(app, sha string) (Blob, error) {
	b := Blob{AppID: app, SHA256: sha}
	if err := requireApp(app); err != nil {
		return b, err
	}
	var at int64
	err := s.DB.QueryRow("SELECT size_bytes,verified_at_s FROM blobs WHERE app_id=? AND sha256=?", app, sha).Scan(&b.SizeBytes, &at)
	b.VerifiedAt = time.Unix(at, 0).UTC()
	return b, err
}
func (s *Store) Blobs() ([]Blob, error) {
	rows, err := s.DB.Query("SELECT app_id,sha256,size_bytes,verified_at_s FROM blobs ORDER BY app_id,sha256")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Blob{}
	for rows.Next() {
		var b Blob
		var at int64
		if err = rows.Scan(&b.AppID, &b.SHA256, &b.SizeBytes, &at); err != nil {
			return nil, err
		}
		b.VerifiedAt = time.Unix(at, 0).UTC()
		out = append(out, b)
	}
	return out, rows.Err()
}
func (s *Store) BlobReferences(app, sha string) (int, error) {
	if err := requireApp(app); err != nil {
		return 0, err
	}
	var n int
	err := s.DB.QueryRow("SELECT COUNT(*) FROM generations WHERE app_id=? AND blob_sha256=?", app, sha).Scan(&n)
	return n, err
}
func (s *Store) DeleteUnreferencedBlob(app, sha string) (bool, error) {
	if err := requireApp(app); err != nil {
		return false, err
	}
	r, err := s.DB.Exec("DELETE FROM blobs WHERE app_id=? AND sha256=? AND NOT EXISTS(SELECT 1 FROM generations WHERE app_id=? AND blob_sha256=?)", app, sha, app, sha)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}

// CompleteGeneration publishes the database association only after the caller has
// verified, fsynced and atomically placed the blob. Retired writers cannot publish.
func (s *Store) CompleteGeneration(app, id string, b Blob, finished time.Time, verificationNS int64) error {
	if b.AppID != app || verificationNS < 0 {
		return errors.New("Blob application mismatch")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var hash string
	var current bool
	var expected *int64
	err = tx.QueryRow("SELECT g.expected_sha256,g.is_current,r.expected_size FROM generations g JOIN resources r ON r.app_id=g.app_id AND r.version=g.version AND r.resource_key=g.resource_key WHERE g.app_id=? AND g.id=?", app, id).Scan(&hash, &current, &expected)
	if err != nil {
		return err
	}
	if !current || hash != b.SHA256 || expected != nil && *expected != b.SizeBytes {
		return errors.New("Generation retired or verified blob does not match authorization")
	}
	if err = putBlob(tx, b); err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE generations SET phase='complete',blob_sha256=?,bytes=?,total_bytes=?,finished_at_s=?,verification_ns=? WHERE id=? AND app_id=? AND is_current=1", b.SHA256, b.SizeBytes, b.SizeBytes, finished.Unix(), verificationNS, id, app)
	if err != nil {
		return err
	}
	return tx.Commit()
}
