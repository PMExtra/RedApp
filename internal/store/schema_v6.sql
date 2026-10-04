PRAGMA foreign_keys=ON;

CREATE TABLE schema_version(version INTEGER NOT NULL CHECK(version=6));

CREATE TABLE directory_state(
  id INTEGER PRIMARY KEY CHECK(id=1), seeded INTEGER NOT NULL CHECK(seeded IN (0,1))
);
CREATE TABLE vendors(
  uid TEXT PRIMARY KEY CHECK(length(uid)=32 AND uid NOT GLOB '*[^0-9a-f]*'),
  id TEXT NOT NULL UNIQUE,
  name_en TEXT NOT NULL, name_zh_cn TEXT NOT NULL,
  description_en TEXT NOT NULL, description_zh_cn TEXT NOT NULL,
  icon TEXT NOT NULL,
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  revision INTEGER NOT NULL CHECK(revision>=1), deleted_at_s INTEGER
);
CREATE TABLE applications(
  uid TEXT PRIMARY KEY CHECK(length(uid)=32 AND uid NOT GLOB '*[^0-9a-f]*'),
  vendor_uid TEXT NOT NULL, id TEXT NOT NULL,
  name_en TEXT NOT NULL, name_zh_cn TEXT NOT NULL,
  description_en TEXT NOT NULL, description_zh_cn TEXT NOT NULL,
  icon TEXT NOT NULL,
  provider TEXT NOT NULL CHECK(provider IN ('info','hosted','http-cache','codex','claude-code')),
  base_url TEXT NOT NULL, cache_ttl_seconds INTEGER NOT NULL CHECK(cache_ttl_seconds BETWEEN 0 AND 86400),
  base_urls_json TEXT NOT NULL,
  source_strategy TEXT NOT NULL CHECK(source_strategy IN ('','ordered','round_robin','random')),
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  revision INTEGER NOT NULL CHECK(revision>=1), source_epoch INTEGER NOT NULL CHECK(source_epoch>=1),
  deleted_at_s INTEGER,
  UNIQUE(vendor_uid,id), FOREIGN KEY(vendor_uid) REFERENCES vendors(uid)
);
CREATE TABLE application_sources(
  app_uid TEXT NOT NULL, epoch INTEGER NOT NULL CHECK(epoch>=1),
  provider TEXT NOT NULL CHECK(provider IN ('http-cache','codex','claude-code')),
  base_url TEXT NOT NULL, created_at_s INTEGER NOT NULL,
  base_urls_json TEXT NOT NULL,
  source_strategy TEXT NOT NULL CHECK(source_strategy IN ('','ordered','round_robin','random')),
  PRIMARY KEY(app_uid,epoch), FOREIGN KEY(app_uid) REFERENCES applications(uid)
);

CREATE TABLE settings(
  scope TEXT NOT NULL, app_id TEXT NOT NULL, key TEXT NOT NULL,
  revision INTEGER NOT NULL CHECK(revision>=1),
  payload BLOB NOT NULL,
  PRIMARY KEY(scope,app_id,key),
  CHECK((scope='global' AND app_id='' AND key IN ('site','upstream_proxy','public_url'))
     OR (scope='app' AND app_id<>'' AND key IN ('channel_ttl','http_policy')))
);
CREATE TABLE app_versions(
  app_id TEXT NOT NULL, version TEXT NOT NULL, first_seen_s INTEGER NOT NULL,
  artifact_requests INTEGER NOT NULL DEFAULT 0 CHECK(artifact_requests>=0),
  downstream_bytes INTEGER NOT NULL DEFAULT 0 CHECK(downstream_bytes>=0),
  PRIMARY KEY(app_id,version)
);
CREATE TABLE release_metadata(
  app_id TEXT NOT NULL, version TEXT NOT NULL,
  raw BLOB NOT NULL, signature BLOB,
  trust_revision INTEGER NOT NULL CHECK(trust_revision>=1),
  fetched_at_s INTEGER NOT NULL,
  PRIMARY KEY(app_id,version),
  FOREIGN KEY(app_id,version) REFERENCES app_versions(app_id,version)
);
CREATE TABLE channels(
  app_id TEXT NOT NULL, channel TEXT NOT NULL, version TEXT NOT NULL,
  fetched_at_s INTEGER NOT NULL, expires_at_s INTEGER NOT NULL,
  PRIMARY KEY(app_id,channel),
  FOREIGN KEY(app_id,version) REFERENCES release_metadata(app_id,version)
);
CREATE TABLE resources(
  app_id TEXT NOT NULL, version TEXT NOT NULL, resource_key TEXT NOT NULL,
  source_url TEXT NOT NULL,
  sha256 TEXT NOT NULL CHECK(length(sha256)=64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
  expected_size INTEGER CHECK(expected_size>=0),
  PRIMARY KEY(app_id,version,resource_key),
  UNIQUE(app_id,version,resource_key,sha256),
  FOREIGN KEY(app_id,version) REFERENCES release_metadata(app_id,version)
);
CREATE TABLE blobs(
  app_id TEXT NOT NULL,
  sha256 TEXT NOT NULL CHECK(length(sha256)=64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
  size_bytes INTEGER NOT NULL CHECK(size_bytes>=0),
  verified_at_s INTEGER NOT NULL,
  PRIMARY KEY(app_id,sha256)
);
CREATE TABLE generations(
  id TEXT PRIMARY KEY,
  app_id TEXT NOT NULL, version TEXT NOT NULL, resource_key TEXT NOT NULL,
  app_revision INTEGER NOT NULL DEFAULT 0 CHECK(app_revision>=0),
  vendor_revision INTEGER NOT NULL DEFAULT 0 CHECK(vendor_revision>=0),
  expected_sha256 TEXT NOT NULL, blob_sha256 TEXT,
  phase TEXT NOT NULL CHECK(phase IN ('incomplete','complete','failed')),
  is_current INTEGER NOT NULL DEFAULT 1 CHECK(is_current IN (0,1)),
  retired_at_s INTEGER,
  bytes INTEGER NOT NULL CHECK(bytes>=0), total_bytes INTEGER CHECK(total_bytes>=0),
  source_bytes INTEGER NOT NULL CHECK(source_bytes>=0),
  full_retry INTEGER NOT NULL DEFAULT 0 CHECK(full_retry IN (0,1)),
  etag TEXT, resumes INTEGER NOT NULL CHECK(resumes>=0),
  started_at_s INTEGER NOT NULL, finished_at_s INTEGER,
  verification_ns INTEGER CHECK(verification_ns>=0), last_error_code TEXT,
  download_ns INTEGER NOT NULL DEFAULT 0 CHECK(download_ns>=0),
  CHECK(is_current=0 OR retired_at_s IS NULL),
  CHECK(blob_sha256 IS NULL OR blob_sha256=expected_sha256),
  CHECK((phase='complete' AND blob_sha256 IS NOT NULL) OR (phase<>'complete' AND blob_sha256 IS NULL)),
  FOREIGN KEY(app_id,version,resource_key,expected_sha256)
    REFERENCES resources(app_id,version,resource_key,sha256),
  FOREIGN KEY(app_id,blob_sha256) REFERENCES blobs(app_id,sha256)
);
CREATE UNIQUE INDEX generations_current
  ON generations(app_id,version,resource_key) WHERE is_current=1;
CREATE TABLE cleanup_previews(
  id TEXT PRIMARY KEY, app_id TEXT NOT NULL,
  app_revision INTEGER NOT NULL DEFAULT 0 CHECK(app_revision>=0),
  vendor_revision INTEGER NOT NULL DEFAULT 0 CHECK(vendor_revision>=0),
  created_at_s INTEGER NOT NULL, expires_at_s INTEGER NOT NULL,
  selection_json BLOB NOT NULL,
  executed_at_s INTEGER, result_json BLOB
);
CREATE TABLE metric_counters(
  scope TEXT NOT NULL, app_id TEXT NOT NULL, metric TEXT NOT NULL,
  value INTEGER NOT NULL CHECK(value>=0), observed_since_s INTEGER NOT NULL,
  CHECK((scope='global' AND app_id='') OR (scope='app' AND app_id<>'')),
  PRIMARY KEY(scope,app_id,metric)
);
CREATE TABLE metric_samples(
  scope TEXT NOT NULL, app_id TEXT NOT NULL, metric TEXT NOT NULL, t_s INTEGER NOT NULL,
  observed_at_s INTEGER NOT NULL, boot TEXT NOT NULL, value REAL NOT NULL,
  delta REAL, duration_s REAL NOT NULL,
  CHECK((scope='global' AND app_id='') OR (scope='app' AND app_id<>'')),
  PRIMARY KEY(scope,app_id,metric,t_s)
);
CREATE TABLE metric_hours(
  scope TEXT NOT NULL, app_id TEXT NOT NULL, metric TEXT NOT NULL, t_s INTEGER NOT NULL,
  min REAL, max REAL, avg REAL, last REAL, count INTEGER NOT NULL,
  delta REAL, delta_count INTEGER NOT NULL, duration_s REAL NOT NULL,
  CHECK((scope='global' AND app_id='') OR (scope='app' AND app_id<>'')),
  PRIMARY KEY(scope,app_id,metric,t_s)
);
CREATE TABLE metric_history_state(
  id INTEGER PRIMARY KEY CHECK(id=1), aggregated_before_s INTEGER NOT NULL
);
CREATE TABLE events(
  id INTEGER PRIMARY KEY, time_s INTEGER NOT NULL, app_id TEXT,
  version TEXT, resource_key TEXT, generation_id TEXT,
  category TEXT NOT NULL, code TEXT NOT NULL, message TEXT NOT NULL,
  upstream_status INTEGER
);
CREATE TABLE admin(
  id INTEGER PRIMARY KEY CHECK(id=1), hash BLOB NOT NULL, revision INTEGER NOT NULL
);

INSERT INTO schema_version VALUES(6);
INSERT INTO directory_state VALUES(1,0);
INSERT INTO metric_history_state VALUES(1,0);
CREATE INDEX metric_samples_time ON metric_samples(t_s);
CREATE INDEX metric_hours_time ON metric_hours(t_s);
CREATE INDEX events_time ON events(time_s);
CREATE INDEX events_app ON events(app_id,id);
CREATE INDEX generations_blob ON generations(app_id,blob_sha256);

-- Mutable HTTP representations have observed hashes, never release authorizations.
CREATE TABLE http_cache_generations(
  row_no INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT NOT NULL UNIQUE CHECK(length(id)=32 AND id NOT GLOB '*[^0-9a-f]*'),
  storage_id TEXT NOT NULL, path TEXT NOT NULL,
  source_url TEXT NOT NULL DEFAULT '',
  sha256 TEXT NOT NULL CHECK(length(sha256)=64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
  size_bytes INTEGER NOT NULL CHECK(size_bytes>=0),
  fetched_at_s INTEGER NOT NULL, validated_at_s INTEGER NOT NULL,
  last_access_bucket_s INTEGER NOT NULL DEFAULT 0,
  fresh_until_s INTEGER NOT NULL, headers_json BLOB NOT NULL,
  is_current INTEGER NOT NULL CHECK(is_current IN (0,1)), retired_at_s INTEGER
);
CREATE UNIQUE INDEX http_cache_current ON http_cache_generations(storage_id,path) WHERE is_current=1;
CREATE INDEX http_cache_scan ON http_cache_generations(storage_id,row_no) WHERE is_current=1;
CREATE TABLE http_cleanup_previews(
  id TEXT PRIMARY KEY, storage_id TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'cleanup' CHECK(kind IN ('cleanup','refresh')),
  state TEXT NOT NULL DEFAULT 'ready' CHECK(state IN ('building','ready','running','done','failed')),
  high_water INTEGER NOT NULL DEFAULT 0 CHECK(high_water>=0),
  scanned_count INTEGER NOT NULL DEFAULT 0 CHECK(scanned_count>=0),
  selected_count INTEGER NOT NULL DEFAULT 0 CHECK(selected_count>=0),
  completed_count INTEGER NOT NULL DEFAULT 0 CHECK(completed_count>=0),
  failed_count INTEGER NOT NULL DEFAULT 0 CHECK(failed_count>=0),
  selected_bytes INTEGER NOT NULL DEFAULT 0 CHECK(selected_bytes>=0),
  app_revision INTEGER NOT NULL, vendor_revision INTEGER NOT NULL,
  created_at_s INTEGER NOT NULL, expires_at_s INTEGER NOT NULL,
  selection_json BLOB NOT NULL,
  executed_at_s INTEGER, result_json BLOB
);
CREATE TABLE http_cleanup_preview_items(
  preview_id TEXT NOT NULL,
  ordinal INTEGER NOT NULL CHECK(ordinal>=0),
  generation_id TEXT NOT NULL,
  path TEXT NOT NULL,
  size_bytes INTEGER NOT NULL CHECK(size_bytes>=0),
  access_bucket_s INTEGER NOT NULL,
  basis TEXT NOT NULL,
  before_s INTEGER NOT NULL,
  match_json BLOB NOT NULL,
  rule_index INTEGER NOT NULL DEFAULT -1 CHECK(rule_index>=-1),
  result_status TEXT NOT NULL DEFAULT 'pending',
  error_code TEXT NOT NULL DEFAULT '',
  PRIMARY KEY(preview_id,ordinal),
  FOREIGN KEY(preview_id) REFERENCES http_cleanup_previews(id) ON DELETE CASCADE
);
CREATE INDEX http_cleanup_preview_pending ON http_cleanup_preview_items(preview_id,result_status,ordinal);

CREATE TABLE application_instructions(
  app_uid TEXT PRIMARY KEY,
  revision INTEGER NOT NULL CHECK(revision>=1),
  en TEXT NOT NULL, zh_cn TEXT NOT NULL,
  FOREIGN KEY(app_uid) REFERENCES applications(uid)
);

CREATE TABLE hosted_files(
  app_uid TEXT NOT NULL,
  path TEXT NOT NULL,
  id TEXT NOT NULL UNIQUE,
  sha256 TEXT NOT NULL,
  size_bytes INTEGER NOT NULL CHECK(size_bytes>=0),
  created_at_s INTEGER NOT NULL,
  PRIMARY KEY(app_uid,path),
  FOREIGN KEY(app_uid) REFERENCES applications(uid)
);

-- Hourly HLL registers merge to estimate distinct download clients over seven days.
CREATE TABLE download_sketches(
 app_uid TEXT NOT NULL, hour_s INTEGER NOT NULL,
 registers BLOB NOT NULL CHECK(length(registers)=1024),
 PRIMARY KEY(app_uid,hour_s), FOREIGN KEY(app_uid) REFERENCES applications(uid)
);
CREATE TABLE catalog_state(
 id INTEGER PRIMARY KEY CHECK(id=1), revision INTEGER NOT NULL CHECK(revision>=0),
 ranking_salt BLOB NOT NULL CHECK(length(ranking_salt)=32)
);
CREATE TABLE homepage_pins(
 app_uid TEXT PRIMARY KEY, position INTEGER NOT NULL UNIQUE CHECK(position>=0),
 FOREIGN KEY(app_uid) REFERENCES applications(uid)
);
INSERT INTO catalog_state VALUES(1,0,randomblob(32));
-- Transactional deletion receipts make physical cleanup restartable after a crash.
CREATE TABLE pending_object_deletes(path TEXT PRIMARY KEY);
