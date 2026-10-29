package store

import "database/sql"

// migrate22To23 adds icon_has_background to applications: no existing icon
// is marked, so every application keeps its padded tile. Keep this SQL
// independent of future schema.
func migrate22To23(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE applications_v23(
  uid TEXT PRIMARY KEY CHECK(length(uid)=32 AND uid NOT GLOB '*[^0-9a-f]*'),
  vendor_uid TEXT NOT NULL REFERENCES vendors(uid),
  id TEXT NOT NULL,
  name_en TEXT NOT NULL,
  name_zh_cn TEXT NOT NULL,
  description_en TEXT NOT NULL,
  description_zh_cn TEXT NOT NULL,
  icon TEXT NOT NULL,
  icon_has_background INTEGER NOT NULL DEFAULT 0 CHECK(icon_has_background IN (0,1)),
  provider TEXT NOT NULL CHECK(provider IN ('info','hosted','http-cache','codex','claude-code','json-release')),
  base_url TEXT NOT NULL,
  base_urls_json TEXT NOT NULL,
  json_release_json TEXT NOT NULL DEFAULT 'null',
  source_strategy TEXT NOT NULL CHECK(source_strategy IN ('','ordered','round_robin','random')),
  cache_ttl_seconds INTEGER NOT NULL CHECK(cache_ttl_seconds BETWEEN 0 AND 86400),
  auto_index INTEGER NOT NULL DEFAULT 0 CHECK(auto_index IN (0,1)),
  default_artifact TEXT NOT NULL DEFAULT '',
  hidden_artifacts_json TEXT NOT NULL DEFAULT '[]',
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  revision INTEGER NOT NULL CHECK(revision>=1),
  runtime_revision INTEGER NOT NULL CHECK(runtime_revision>=1),
  source_epoch INTEGER NOT NULL CHECK(source_epoch>=1),
  deleted_at_s INTEGER,
  UNIQUE(vendor_uid,id)
);
INSERT INTO applications_v23(uid,vendor_uid,id,name_en,name_zh_cn,description_en,description_zh_cn,icon,provider,base_url,base_urls_json,json_release_json,source_strategy,cache_ttl_seconds,auto_index,default_artifact,hidden_artifacts_json,enabled,revision,runtime_revision,source_epoch,deleted_at_s) SELECT uid,vendor_uid,id,name_en,name_zh_cn,description_en,description_zh_cn,icon,provider,base_url,base_urls_json,json_release_json,source_strategy,cache_ttl_seconds,auto_index,default_artifact,hidden_artifacts_json,enabled,revision,runtime_revision,source_epoch,deleted_at_s FROM applications;
DROP TABLE applications;
ALTER TABLE applications_v23 RENAME TO applications;
`)
	return err
}
