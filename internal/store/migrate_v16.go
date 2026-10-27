package store

import "database/sql"

// migrate15To16 preserves all original columns and identities. The migration
// framework disables foreign keys before BEGIN, backs up the database, and
// validates every foreign key and the complete schema before COMMIT. No blob
// paths or file contents are changed. Keep this SQL independent of future schema.
func migrate15To16(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE applications_v16(
  uid TEXT PRIMARY KEY CHECK(length(uid)=32 AND uid NOT GLOB '*[^0-9a-f]*'),
  vendor_uid TEXT NOT NULL REFERENCES vendors(uid),
  id TEXT NOT NULL,
  name_en TEXT NOT NULL,
  name_zh_cn TEXT NOT NULL,
  description_en TEXT NOT NULL,
  description_zh_cn TEXT NOT NULL,
  icon TEXT NOT NULL,
  provider TEXT NOT NULL CHECK(provider IN ('info','hosted','http-cache','codex','claude-code','json-release')),
  base_url TEXT NOT NULL,
  base_urls_json TEXT NOT NULL,
  json_release_json TEXT NOT NULL DEFAULT 'null',
  source_strategy TEXT NOT NULL CHECK(source_strategy IN ('','ordered','round_robin','random')),
  cache_ttl_seconds INTEGER NOT NULL CHECK(cache_ttl_seconds BETWEEN 0 AND 86400),
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  revision INTEGER NOT NULL CHECK(revision>=1),
  runtime_revision INTEGER NOT NULL CHECK(runtime_revision>=1),
  source_epoch INTEGER NOT NULL CHECK(source_epoch>=1),
  deleted_at_s INTEGER,
  UNIQUE(vendor_uid,id)
);
INSERT INTO applications_v16(uid,vendor_uid,id,name_en,name_zh_cn,description_en,description_zh_cn,icon,provider,base_url,base_urls_json,source_strategy,cache_ttl_seconds,enabled,revision,runtime_revision,source_epoch,deleted_at_s) SELECT uid,vendor_uid,id,name_en,name_zh_cn,description_en,description_zh_cn,icon,provider,base_url,base_urls_json,source_strategy,cache_ttl_seconds,enabled,revision,runtime_revision,source_epoch,deleted_at_s FROM applications;
DROP TABLE applications;
ALTER TABLE applications_v16 RENAME TO applications;
CREATE TABLE application_sources_v16(
  app_uid TEXT NOT NULL REFERENCES applications(uid) ON DELETE CASCADE,
  epoch INTEGER NOT NULL CHECK(epoch>=1),
  provider TEXT NOT NULL CHECK(provider IN ('http-cache','codex','claude-code','json-release')),
  base_url TEXT NOT NULL,
  base_urls_json TEXT NOT NULL,
  json_release_json TEXT NOT NULL DEFAULT 'null',
  source_strategy TEXT NOT NULL CHECK(source_strategy IN ('','ordered','round_robin','random')),
  created_at_s INTEGER NOT NULL,
  PRIMARY KEY(app_uid,epoch)
);
INSERT INTO application_sources_v16(app_uid,epoch,provider,base_url,base_urls_json,source_strategy,created_at_s) SELECT app_uid,epoch,provider,base_url,base_urls_json,source_strategy,created_at_s FROM application_sources;
DROP TABLE application_sources;
ALTER TABLE application_sources_v16 RENAME TO application_sources;
CREATE TABLE release_refresh_status(
 app_uid TEXT NOT NULL,
 source_epoch INTEGER NOT NULL,
 channel TEXT NOT NULL,
 attempted_at_s INTEGER NOT NULL,
 succeeded_at_s INTEGER,
 failed INTEGER NOT NULL CHECK(failed IN (0,1)),
 PRIMARY KEY(app_uid,source_epoch,channel),
 FOREIGN KEY(app_uid,source_epoch) REFERENCES application_sources(app_uid,epoch) ON DELETE CASCADE
);
`)
	return err
}
