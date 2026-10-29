package store

import "database/sql"

// migrate21To22 adds the global redirect protection setting. Its default is
// strict, which every vendor and application inherits: release applications
// keep following only their sources, while HTTP cache applications, which
// followed HTTPS redirects to any public host, now follow only their own base
// and allowlist until an administrator relaxes them. Keep this SQL
// independent of future schema.
func migrate21To22(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE settings_v22(
  key TEXT PRIMARY KEY CHECK(key IN ('site','upstream_proxy','public_url','redirect_protection')),
  revision INTEGER NOT NULL CHECK(revision>=1),
  payload BLOB NOT NULL
);
INSERT INTO settings_v22(key,revision,payload) SELECT key,revision,payload FROM settings;
DROP TABLE settings;
ALTER TABLE settings_v22 RENAME TO settings;
INSERT OR IGNORE INTO settings(key,revision,payload) VALUES('redirect_protection',1,'{"mode":"strict"}');
`)
	return err
}
