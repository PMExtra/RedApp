package store

import (
	"bytes"
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
		return errors.New("invalid authorized resource")
	}
	for _, c := range r.SHA256 {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return errors.New("invalid resource digest")
		}
	}
	if r.ExpectedSize != nil && *r.ExpectedSize < 0 {
		return errors.New("invalid resource size")
	}
	return nil
}
func equalSize(a, b *int64) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

// PutRelease atomically commits trusted bytes, discovery history and an immutable
// resource set. Trust revalidation may replace envelope bytes but never bindings.
func (s *Store) PutRelease(m ReleaseMetadata, resources []Resource, expected ...SourceFence) error {
	return s.putRelease(m, resources, nil, expected...)
}

// PutReleaseChannel commits a verified release, its immutable resources and one
// channel head in the same transaction. Failed refreshes cannot move the head
// or leave a newly discovered partial version. Existing providers keep using
// PutRelease/PutChannel until explicitly migrated to this contract.
func (s *Store) PutReleaseChannel(m ReleaseMetadata, resources []Resource, channel Channel, expected ...SourceFence) error {
	if channel.AppID != m.AppID || channel.Version != m.Version || channel.Name == "" || !channel.FetchedAt.Equal(m.FetchedAt) || !channel.ExpiresAt.After(channel.FetchedAt) {
		return errors.New("channel does not describe the verified release")
	}
	return s.putRelease(m, resources, &channel, expected...)
}

func (s *Store) putRelease(m ReleaseMetadata, resources []Resource, channel *Channel, expected ...SourceFence) error {
	if requireApp(m.AppID) != nil || m.Version == "" || m.Raw == nil || m.TrustRevision < 1 || m.FetchedAt.IsZero() {
		return errors.New("invalid trusted release")
	}
	seen := map[string]bool{}
	for _, r := range resources {
		if err := validateResource(r); err != nil {
			return err
		}
		if r.AppID != m.AppID || r.Version != m.Version || seen[r.Key] {
			return errors.New("release resource identity mismatch")
		}
		seen[r.Key] = true
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.requireSourceActive(tx, m.AppID, expected...); err != nil {
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
		// Atomic snapshot publication also freezes normalized dimensions and
		// identity fields, not only download bindings. Query snapshots must be
		// canonicalized before calling PutReleaseChannel.
		if channel != nil && (!bytes.Equal(oldRaw, m.Raw) || !bytes.Equal(oldSig, m.Signature)) {
			return ErrImmutableRelease
		}
		if m.TrustRevision < oldRevision {
			return errors.New("trust revision cannot move backwards")
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
	if channel != nil {
		if _, err = tx.Exec("INSERT INTO channels(app_id,channel,version,fetched_at_s,expires_at_s) VALUES(?,?,?,?,?) ON CONFLICT(app_id,channel) DO UPDATE SET version=excluded.version,fetched_at_s=excluded.fetched_at_s,expires_at_s=excluded.expires_at_s", channel.AppID, channel.Name, channel.Version, channel.FetchedAt.Unix(), channel.ExpiresAt.Unix()); err != nil {
			return err
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
	err := s.read.QueryRow("SELECT raw,signature,trust_revision,fetched_at_s FROM release_metadata WHERE app_id=? AND version=?", app, version).Scan(&m.Raw, &m.Signature, &m.TrustRevision, &at)
	m.FetchedAt = time.Unix(at, 0).UTC()
	return m, err
}

// ErrReleaseInUse reports a release that still has download generations.
var ErrReleaseInUse = errors.New("the release still has cached files")

// ReplaceRelease replaces a stored release with newly verified metadata and
// resources in one transaction, and moves the channels that pointed at it to
// the new metadata with a fresh expiry; it returns those channels. The old
// release's files must be gone first, because generations reference its
// resources. Its version statistics and first-seen time stay.
func (s *Store) ReplaceRelease(m ReleaseMetadata, resources []Resource, ttl time.Duration, expected ...SourceFence) ([]string, error) {
	if requireApp(m.AppID) != nil || m.Version == "" || m.Raw == nil || m.TrustRevision < 1 || m.FetchedAt.IsZero() || ttl <= 0 {
		return nil, errors.New("invalid trusted release")
	}
	for _, r := range resources {
		if err := validateResource(r); err != nil {
			return nil, err
		}
		if r.AppID != m.AppID || r.Version != m.Version {
			return nil, errors.New("release resource identity mismatch")
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = s.requireSourceActive(tx, m.AppID, expected...); err != nil {
		return nil, err
	}
	var exists, generations int
	if err = tx.QueryRow("SELECT (SELECT count(*) FROM release_metadata WHERE app_id=? AND version=?),(SELECT count(*) FROM generations WHERE app_id=? AND version=?)", m.AppID, m.Version, m.AppID, m.Version).Scan(&exists, &generations); err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, ErrNotFound
	}
	if generations != 0 {
		return nil, ErrReleaseInUse
	}
	rows, err := tx.Query("SELECT channel FROM channels WHERE app_id=? AND version=? ORDER BY channel", m.AppID, m.Version)
	if err != nil {
		return nil, err
	}
	channels := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		channels = append(channels, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Resources and channel heads cascade; they are written again below.
	if _, err = tx.Exec("DELETE FROM release_metadata WHERE app_id=? AND version=?", m.AppID, m.Version); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(`INSERT INTO release_metadata(app_id,version,raw,signature,trust_revision,fetched_at_s) VALUES(?,?,?,?,?,?)`, m.AppID, m.Version, m.Raw, m.Signature, m.TrustRevision, m.FetchedAt.Unix()); err != nil {
		return nil, err
	}
	for _, r := range resources {
		if _, err = tx.Exec("INSERT INTO resources(app_id,version,resource_key,source_url,sha256,expected_size) VALUES(?,?,?,?,?,?)", r.AppID, r.Version, r.Key, r.SourceURL, r.SHA256, r.ExpectedSize); err != nil {
			return nil, err
		}
	}
	for _, name := range channels {
		if _, err = tx.Exec("INSERT INTO channels(app_id,channel,version,fetched_at_s,expires_at_s) VALUES(?,?,?,?,?)", m.AppID, name, m.Version, m.FetchedAt.Unix(), m.FetchedAt.Add(ttl).Unix()); err != nil {
			return nil, err
		}
	}
	return channels, tx.Commit()
}

// StoredRelease is stored release metadata and when the release was first
// seen.
type StoredRelease struct {
	ReleaseMetadata
	FirstSeen time.Time
}

// Releases lists every stored release of an application storage ID, most
// recently first seen first.
func (s *Store) Releases(app string) ([]StoredRelease, error) {
	if err := requireApp(app); err != nil {
		return nil, err
	}
	rows, err := s.read.Query("SELECT m.version,m.raw,m.signature,m.trust_revision,m.fetched_at_s,v.first_seen_s FROM release_metadata m JOIN app_versions v ON v.app_id=m.app_id AND v.version=m.version WHERE m.app_id=? ORDER BY v.first_seen_s DESC,m.version", app)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoredRelease
	for rows.Next() {
		r := StoredRelease{ReleaseMetadata: ReleaseMetadata{AppID: app}}
		var fetched, seen int64
		if err = rows.Scan(&r.Version, &r.Raw, &r.Signature, &r.TrustRevision, &fetched, &seen); err != nil {
			return nil, err
		}
		r.FetchedAt, r.FirstSeen = time.Unix(fetched, 0).UTC(), time.Unix(seen, 0).UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) Resource(app, version, key string) (Resource, error) {
	r := Resource{AppID: app, Version: version, Key: key}
	if err := requireApp(app); err != nil {
		return r, err
	}
	err := s.read.QueryRow("SELECT source_url,sha256,expected_size FROM resources WHERE app_id=? AND version=? AND resource_key=?", app, version, key).Scan(&r.SourceURL, &r.SHA256, &r.ExpectedSize)
	return r, err
}
func (s *Store) Resources(app, version string) ([]Resource, error) {
	if err := requireApp(app); err != nil {
		return nil, err
	}
	rows, err := s.read.Query("SELECT resource_key,source_url,sha256,expected_size FROM resources WHERE app_id=? AND version=? ORDER BY resource_key", app, version)
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
		return errors.New("invalid channel record")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.requireSourceActive(tx, c.AppID, expected...); err != nil {
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
	err := s.read.QueryRow("SELECT version,fetched_at_s,expires_at_s FROM channels WHERE app_id=? AND channel=?", app, name).Scan(&c.Version, &fetched, &expires)
	c.FetchedAt = time.Unix(fetched, 0).UTC()
	c.ExpiresAt = time.Unix(expires, 0).UTC()
	return c, err
}
func (s *Store) SeenFor(app, version string) error {
	if requireApp(app) != nil || version == "" {
		return errors.New("application and version are required")
	}
	_, err := s.db.Exec("INSERT OR IGNORE INTO app_versions(app_id,version,first_seen_s) VALUES(?,?,?)", app, version, time.Now().Unix())
	return err
}
func (s *Store) VersionsFor(app string) (map[string]string, error) {
	if err := requireApp(app); err != nil {
		return nil, err
	}
	rows, err := s.read.Query("SELECT version,first_seen_s FROM app_versions WHERE app_id=?", app)
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
	rows, err := s.read.Query("SELECT version,first_seen_s,artifact_requests,downstream_bytes FROM app_versions WHERE app_id=?", app)
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
