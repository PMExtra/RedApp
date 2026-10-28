package store

import "database/sql"

// migrate19To20 adds upstream instances and mirror bindings; nothing is bound
// yet. Keep this SQL independent of future schema.
func migrate19To20(tx *sql.Tx) error {
	_, err := tx.Exec(`
-- Upstream RedApp instances whose applications this instance mirrors. origin
-- is scheme://host[:port]; instance_id is the identity it last reported.
CREATE TABLE upstreams(
  uid TEXT PRIMARY KEY CHECK(length(uid)=32 AND uid NOT GLOB '*[^0-9a-f]*'),
  name TEXT NOT NULL,
  origin TEXT NOT NULL UNIQUE,
  instance_id TEXT NOT NULL CHECK(length(instance_id)=32 AND instance_id NOT GLOB '*[^0-9a-f]*'),
  revision INTEGER NOT NULL CHECK(revision>=1),
  created_at_s INTEGER NOT NULL
);
-- A mirrored application follows the template upstream:<app_uid>, refreshed
-- from the mirror description of remote_key on its upstream. A referenced
-- upstream cannot be deleted.
CREATE TABLE application_upstreams(
  app_uid TEXT PRIMARY KEY REFERENCES applications(uid) ON DELETE CASCADE,
  upstream_uid TEXT NOT NULL REFERENCES upstreams(uid),
  remote_key TEXT NOT NULL,
  remote_revision TEXT NOT NULL,
  remote_icon TEXT NOT NULL,
  upstream_chain_json TEXT NOT NULL,
  synced_at_s INTEGER NOT NULL,
  checked_at_s INTEGER NOT NULL,
  error_code TEXT NOT NULL,
  UNIQUE(upstream_uid,remote_key)
);
`)
	return err
}
