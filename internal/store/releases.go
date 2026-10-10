package store

import (
	"database/sql"
	"errors"
	"time"
)

type ReleaseMetadata struct {
	AppID, Version string
	Raw, Signature []byte
	TrustRevision  int64
	FetchedAt      time.Time
}
type Resource struct {
	AppID, Version, Key, SourceURL, SHA256 string
	ExpectedSize                           *int64
}
type Channel struct {
	AppID, Name, Version string
	FetchedAt, ExpiresAt time.Time
}
type VersionStats struct {
	Version          string    `json:"version"`
	FirstSeen        time.Time `json:"first_seen"`
	ArtifactRequests int64     `json:"artifact_requests"`
	DownstreamBytes  int64     `json:"downstream_bytes"`
}

func validateResource(r Resource) error {
	if requireApp(r.AppID) != nil || r.Version == "" || r.Key == "" || r.SourceURL == "" || len(r.SHA256) != 64 {
		return errors.New("Invalid authorized resource")
	}
	for _, c := range r.SHA256 {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return errors.New("Invalid resource digest")
		}
	}
	if r.ExpectedSize != nil && *r.ExpectedSize < 0 {
		return errors.New("Invalid resource size")
	}
	return nil
}
func equalSize(a, b *int64) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

// PutRelease atomically commits trusted bytes, discovery history and an immutable
// resource set. Trust revalidation may replace envelope bytes but never bindings.
func (s *Store) PutRelease(m ReleaseMetadata, resources []Resource, expected ...SourceFence) error {
	if requireApp(m.AppID) != nil || m.Version == "" || m.Raw == nil || m.TrustRevision < 1 || m.FetchedAt.IsZero() {
		return errors.New("Invalid trusted release")
	}
	seen := map[string]bool{}
	for _, r := range resources {
		if err := validateResource(r); err != nil {
			return err
		}
		if r.AppID != m.AppID || r.Version != m.Version || seen[r.Key] {
			return errors.New("Release resource identity mismatch")
		}
		seen[r.Key] = true
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.RequireSourceActive(tx, m.AppID, expected...); err != nil {
		return err
	}
	var oldRaw, oldSig []byte
	var oldRevision int64
	err = tx.QueryRow("SELECT raw,signature,trust_revision FROM release_metadata WHERE app_id=? AND version=?", m.AppID, m.Version).Scan(&oldRaw, &oldSig, &oldRevision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	existing := err == nil
	if existing {
		if m.TrustRevision < oldRevision {
			return errors.New("Trust revision cannot move backwards")
		}
		rows, e := tx.Query("SELECT resource_key,source_url,sha256,expected_size FROM resources WHERE app_id=? AND version=?", m.AppID, m.Version)
		if e != nil {
			return e
		}
		bindings := map[string]Resource{}
		for rows.Next() {
			var r Resource
			if e = rows.Scan(&r.Key, &r.SourceURL, &r.SHA256, &r.ExpectedSize); e != nil {
				rows.Close()
				return e
			}
			bindings[r.Key] = r
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(bindings) != len(resources) {
			return ErrImmutableRelease
		}
		for _, r := range resources {
			old, ok := bindings[r.Key]
			if !ok || old.SourceURL != r.SourceURL || old.SHA256 != r.SHA256 || !equalSize(old.ExpectedSize, r.ExpectedSize) {
				return ErrImmutableRelease
			}
		}
		// Raw metadata may contain refreshed signatures; semantic bindings above are
		// the immutable authorization identity. Preserve first_seen on every refresh.
	}
	if _, err = tx.Exec("INSERT OR IGNORE INTO app_versions(app_id,version,first_seen_s) VALUES(?,?,?)", m.AppID, m.Version, m.FetchedAt.Unix()); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO release_metadata(app_id,version,raw,signature,trust_revision,fetched_at_s) VALUES(?,?,?,?,?,?) ON CONFLICT(app_id,version) DO UPDATE SET raw=excluded.raw,signature=excluded.signature,trust_revision=excluded.trust_revision,fetched_at_s=excluded.fetched_at_s`, m.AppID, m.Version, m.Raw, m.Signature, m.TrustRevision, m.FetchedAt.Unix()); err != nil {
		return err
	}
	if !existing {
		for _, r := range resources {
			if _, err = tx.Exec("INSERT INTO resources(app_id,version,resource_key,source_url,sha256,expected_size) VALUES(?,?,?,?,?,?)", r.AppID, r.Version, r.Key, r.SourceURL, r.SHA256, r.ExpectedSize); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
func (s *Store) Release(app, version string) (ReleaseMetadata, error) {
	var m ReleaseMetadata
	if err := requireApp(app); err != nil {
		return m, err
	}
	m.AppID, m.Version = app, version
	var at int64
	err := s.DB.QueryRow("SELECT raw,signature,trust_revision,fetched_at_s FROM release_metadata WHERE app_id=? AND version=?", app, version).Scan(&m.Raw, &m.Signature, &m.TrustRevision, &at)
	m.FetchedAt = time.Unix(at, 0).UTC()
	return m, err
}
func (s *Store) Resource(app, version, key string) (Resource, error) {
	r := Resource{AppID: app, Version: version, Key: key}
	if err := requireApp(app); err != nil {
		return r, err
	}
	err := s.DB.QueryRow("SELECT source_url,sha256,expected_size FROM resources WHERE app_id=? AND version=? AND resource_key=?", app, version, key).Scan(&r.SourceURL, &r.SHA256, &r.ExpectedSize)
	return r, err
}
func (s *Store) Resources(app, version string) ([]Resource, error) {
	if err := requireApp(app); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query("SELECT resource_key,source_url,sha256,expected_size FROM resources WHERE app_id=? AND version=? ORDER BY resource_key", app, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Resource{}
	for rows.Next() {
		r := Resource{AppID: app, Version: version}
		if err = rows.Scan(&r.Key, &r.SourceURL, &r.SHA256, &r.ExpectedSize); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) PutChannel(c Channel, expected ...SourceFence) error {
	if requireApp(c.AppID) != nil || c.Name == "" || c.Version == "" || !c.ExpiresAt.After(c.FetchedAt) {
		return errors.New("Invalid channel record")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.RequireSourceActive(tx, c.AppID, expected...); err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO channels(app_id,channel,version,fetched_at_s,expires_at_s) VALUES(?,?,?,?,?) ON CONFLICT(app_id,channel) DO UPDATE SET version=excluded.version,fetched_at_s=excluded.fetched_at_s,expires_at_s=excluded.expires_at_s", c.AppID, c.Name, c.Version, c.FetchedAt.Unix(), c.ExpiresAt.Unix())
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) Channel(app, name string) (Channel, error) {
	c := Channel{AppID: app, Name: name}
	if err := requireApp(app); err != nil {
		return c, err
	}
	var fetched, expires int64
	err := s.DB.QueryRow("SELECT version,fetched_at_s,expires_at_s FROM channels WHERE app_id=? AND channel=?", app, name).Scan(&c.Version, &fetched, &expires)
	c.FetchedAt = time.Unix(fetched, 0).UTC()
	c.ExpiresAt = time.Unix(expires, 0).UTC()
	return c, err
}
func (s *Store) SeenFor(app, version string) error {
	if requireApp(app) != nil || version == "" {
		return errors.New("Application and version are required")
	}
	_, err := s.DB.Exec("INSERT OR IGNORE INTO app_versions(app_id,version,first_seen_s) VALUES(?,?,?)", app, version, time.Now().Unix())
	return err
}
func (s *Store) VersionsFor(app string) (map[string]string, error) {
	if err := requireApp(app); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query("SELECT version,first_seen_s FROM app_versions WHERE app_id=?", app)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var v string
		var at int64
		if err = rows.Scan(&v, &at); err != nil {
			return nil, err
		}
		out[v] = time.Unix(at, 0).UTC().Format(time.RFC3339)
	}
	return out, rows.Err()
}
func (s *Store) VersionStats(app string) (map[string]VersionStats, error) {
	if err := requireApp(app); err != nil {
		return nil, err
	}
	s.SettleCounters()
	rows, err := s.DB.Query("SELECT version,first_seen_s,artifact_requests,downstream_bytes FROM app_versions WHERE app_id=?", app)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]VersionStats{}
	for rows.Next() {
		var v VersionStats
		var at int64
		if err = rows.Scan(&v.Version, &at, &v.ArtifactRequests, &v.DownstreamBytes); err != nil {
			return nil, err
		}
		v.FirstSeen = time.Unix(at, 0).UTC()
		out[v.Version] = v
	}
	return out, rows.Err()
}
