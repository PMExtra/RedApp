package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/PMExtra/RedApp/internal/warmplan"
	"time"
)

type PrewarmJob struct {
	SourceFence
	ResolvedVersion    string          `json:"resolved_version,omitempty"`
	ID                 string          `json:"id"`
	AppUID             string          `json:"-"`
	StorageID          string          `json:"-"`
	RequestID          string          `json:"-"`
	Fingerprint        string          `json:"-"`
	PolicyHash         string          `json:"-"`
	SuccessFingerprint string          `json:"-"`
	State              string          `json:"state"`
	Created            time.Time       `json:"created"`
	Updated            time.Time       `json:"updated"`
	Input              warmplan.Input  `json:"-"`
	Completed          int             `json:"completed"`
	Succeeded          int             `json:"succeeded"`
	Bytes              int64           `json:"bytes"`
	Reason             string          `json:"reason,omitempty"`
	Ignored            map[string]int  `json:"ignored"`
	Automatic          bool            `json:"automatic"`
	Target             string          `json:"target,omitempty"`
	Platforms          []string        `json:"platforms,omitempty"`
	Limits             warmplan.Limits `json:"limits"`
}

const prewarmColumns = `id,app_uid,storage_id,request_id,fingerprint,policy_hash,resolved_version,success_fingerprint,state,created_s,updated_s,input_json,completed,succeeded,read_bytes,reason,ignored_json,automatic,app_revision,vendor_revision`

func scanPrewarm(row scanner) (j PrewarmJob, err error) {
	var created, updated int64
	var input, ignored []byte
	err = row.Scan(&j.ID, &j.AppUID, &j.StorageID, &j.RequestID, &j.Fingerprint, &j.PolicyHash, &j.ResolvedVersion, &j.SuccessFingerprint, &j.State, &created, &updated, &input, &j.Completed, &j.Succeeded, &j.Bytes, &j.Reason, &ignored, &j.Automatic, &j.AppRevision, &j.VendorRevision)
	if err != nil {
		return
	}
	if err = json.Unmarshal(input, &j.Input); err != nil {
		return
	}
	if err = json.Unmarshal(ignored, &j.Ignored); err != nil {
		return
	}
	j.Created = time.Unix(created, 0).UTC()
	j.Updated = time.Unix(updated, 0).UTC()
	j.Target = j.Input.Target
	j.Platforms = j.Input.Platforms
	j.Limits = j.Input.Limits
	return
}
func (s *Store) PrewarmJob(uid, id string) (PrewarmJob, error) {
	return scanPrewarm(s.DB.QueryRow(`SELECT `+prewarmColumns+` FROM prewarm_jobs WHERE app_uid=? AND id=?`, uid, id))
}
func (s *Store) PrewarmRequest(uid, id string) (PrewarmJob, error) {
	return scanPrewarm(s.DB.QueryRow(`SELECT `+prewarmColumns+` FROM prewarm_jobs WHERE app_uid=? AND request_id=?`, uid, id))
}
func (s *Store) CreatePrewarm(j PrewarmJob) error {
	input, _ := json.Marshal(j.Input)
	ignored, _ := json.Marshal(j.Ignored)
	_, err := s.DB.Exec(`INSERT INTO prewarm_jobs(`+prewarmColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, j.ID, j.AppUID, j.StorageID, j.RequestID, j.Fingerprint, j.PolicyHash, j.ResolvedVersion, j.SuccessFingerprint, j.State, j.Created.Unix(), j.Updated.Unix(), input, j.Completed, j.Succeeded, j.Bytes, j.Reason, ignored, j.Automatic, j.AppRevision, j.VendorRevision)
	return err
}
func (s *Store) UpdatePrewarm(j PrewarmJob) error {
	ignored, _ := json.Marshal(j.Ignored)
	_, err := s.DB.Exec(`UPDATE prewarm_jobs SET state=?,updated_s=?,completed=?,succeeded=?,read_bytes=?,reason=?,ignored_json=?,success_fingerprint=?,resolved_version=? WHERE id=? AND app_uid=?`, j.State, j.Updated.Unix(), j.Completed, j.Succeeded, j.Bytes, j.Reason, ignored, j.SuccessFingerprint, j.ResolvedVersion, j.ID, j.AppUID)
	return err
}
func (s *Store) AddPrewarmItem(id string, ordinal int, item warmplan.Item) error {
	_, err := s.DB.Exec(`INSERT INTO prewarm_items(job_id,ordinal,item_key,status,reason,read_bytes) VALUES(?,?,?,?,?,?) ON CONFLICT(job_id,ordinal) DO UPDATE SET status=excluded.status,reason=excluded.reason,read_bytes=excluded.read_bytes`, id, ordinal, item.Key, item.Status, item.Reason, item.Bytes)
	return err
}
func (s *Store) PrewarmItems(uid, id string, page, limit int) ([]warmplan.Item, int, error) {
	if page < 1 || limit < 1 || limit > 100 {
		return nil, 0, ErrInvalidDirectory
	}
	var count int
	if err := s.DB.QueryRow(`SELECT count(*) FROM prewarm_items WHERE job_id=? AND EXISTS(SELECT 1 FROM prewarm_jobs WHERE id=? AND app_uid=?)`, id, id, uid).Scan(&count); err != nil {
		return nil, 0, err
	}
	items := []warmplan.Item{}
	if page > max(1, (count+limit-1)/limit) {
		return items, count, nil
	}
	rows, err := s.DB.Query(`SELECT item_key,status,reason,read_bytes FROM prewarm_items WHERE job_id=? AND EXISTS(SELECT 1 FROM prewarm_jobs WHERE id=? AND app_uid=?) ORDER BY ordinal LIMIT ? OFFSET ?`, id, id, uid, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var item warmplan.Item
		if err = rows.Scan(&item.Key, &item.Status, &item.Reason, &item.Bytes); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, count, rows.Err()
}
func (s *Store) RecoverPrewarm() error {
	_, err := s.DB.Exec(`UPDATE prewarm_jobs SET state='interrupted',reason='interrupted_by_restart',updated_s=? WHERE state='running'`, time.Now().Unix())
	return err
}
func (s *Store) PrunePrewarm() error {
	_, err := s.DB.Exec(`DELETE FROM prewarm_jobs WHERE id IN (SELECT id FROM prewarm_jobs WHERE state!='running' AND updated_s<=? ORDER BY updated_s,id LIMIT 10)`, time.Now().Add(-24*time.Hour).Unix())
	return err
}
func (s *Store) PrewarmSuccess(uid, channel, fingerprint string) (bool, error) {
	var value string
	err := s.DB.QueryRow(`SELECT fingerprint FROM prewarm_success WHERE app_uid=? AND channel=?`, uid, channel).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return value == fingerprint, err
}
func (s *Store) SavePrewarmSuccess(uid, channel, fingerprint string) error {
	_, err := s.DB.Exec(`INSERT INTO prewarm_success(app_uid,channel,fingerprint) VALUES(?,?,?) ON CONFLICT(app_uid,channel) DO UPDATE SET fingerprint=excluded.fingerprint`, uid, channel, fingerprint)
	return err
}
