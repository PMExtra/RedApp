package store

import (
	"database/sql"
	"github.com/PMExtra/RedApp/internal/identity"
	"time"
)

type ReleaseRefreshStatus struct {
	Attempt time.Time  `json:"attempt"`
	Success *time.Time `json:"success"`
	Failed  bool       `json:"failed"`
}

func (s *Store) RecordReleaseRefresh(storage, channel string, success bool) error {
	uid, epoch, ok := identity.ParseStorageID(storage)
	if !ok || channel == "" {
		return ErrInvalidDirectory
	}
	now := time.Now().UTC().Unix()
	var succeeded any
	if success {
		succeeded = now
	}
	_, err := s.db.Exec(`INSERT INTO release_refresh_status(app_uid,source_epoch,channel,attempted_at_s,succeeded_at_s,failed) VALUES(?,?,?,?,?,?) ON CONFLICT(app_uid,source_epoch,channel) DO UPDATE SET attempted_at_s=excluded.attempted_at_s,succeeded_at_s=COALESCE(excluded.succeeded_at_s,release_refresh_status.succeeded_at_s),failed=excluded.failed`, uid, epoch, channel, now, succeeded, !success)
	return err
}
func (s *Store) ReleaseRefresh(storage, channel string) (ReleaseRefreshStatus, error) {
	uid, epoch, ok := identity.ParseStorageID(storage)
	if !ok {
		return ReleaseRefreshStatus{}, ErrInvalidDirectory
	}
	var status ReleaseRefreshStatus
	var attempt int64
	var success sql.NullInt64
	err := s.read.QueryRow(`SELECT attempted_at_s,succeeded_at_s,failed FROM release_refresh_status WHERE app_uid=? AND source_epoch=? AND channel=?`, uid, epoch, channel).Scan(&attempt, &success, &status.Failed)
	status.Attempt = time.Unix(attempt, 0).UTC()
	status.Success = timePointer(success)
	return status, err
}
