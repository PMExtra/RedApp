-- RedApp state database. Open writes store.SchemaVersion to PRAGMA user_version
-- and refuses any database with another version (ADR 0001).
--
-- Rows that belong to an application or vendor reference it by UID with
-- ON DELETE CASCADE. Release, cache and metric rows are keyed by the private
-- storage namespace (app/<uid>-e<epoch>) or metrics namespace (app/<uid>)
-- instead, so permanent deletion removes them by prefix.
--
-- The app_revision/vendor_revision columns of generations, cleanup previews and
-- prewarm jobs hold the runtime revisions captured when the work was admitted
-- (store.SourceFence), never configuration revisions.

-- Directory ------------------------------------------------------------------

CREATE TABLE vendors(
  uid TEXT PRIMARY KEY CHECK(length(uid)=32 AND uid NOT GLOB '*[^0-9a-f]*'),
  id TEXT NOT NULL UNIQUE,
  name_en TEXT NOT NULL,
  name_zh_cn TEXT NOT NULL,
  description_en TEXT NOT NULL,
  description_zh_cn TEXT NOT NULL,
  icon TEXT NOT NULL,
  icon_en TEXT NOT NULL,
  icon_zh_cn TEXT NOT NULL,
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  revision INTEGER NOT NULL CHECK(revision>=1),
  runtime_revision INTEGER NOT NULL CHECK(runtime_revision>=1),
  deleted_at_s INTEGER
);
-- A vendor can only be deleted after its applications, whose object files must
-- be queued for deletion first, so this reference deliberately does not cascade.
CREATE TABLE applications(
  uid TEXT PRIMARY KEY CHECK(length(uid)=32 AND uid NOT GLOB '*[^0-9a-f]*'),
  vendor_uid TEXT NOT NULL REFERENCES vendors(uid),
  id TEXT NOT NULL,
  name_en TEXT NOT NULL,
  name_zh_cn TEXT NOT NULL,
  description_en TEXT NOT NULL,
  description_zh_cn TEXT NOT NULL,
  icon TEXT NOT NULL,
  provider TEXT NOT NULL CHECK(provider IN ('info','hosted','http-cache','codex','claude-code')),
  base_url TEXT NOT NULL,
  base_urls_json TEXT NOT NULL,
  source_strategy TEXT NOT NULL CHECK(source_strategy IN ('','ordered','round_robin','random')),
  cache_ttl_seconds INTEGER NOT NULL CHECK(cache_ttl_seconds BETWEEN 0 AND 86400),
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
  revision INTEGER NOT NULL CHECK(revision>=1),
  runtime_revision INTEGER NOT NULL CHECK(runtime_revision>=1),
  source_epoch INTEGER NOT NULL CHECK(source_epoch>=1),
  deleted_at_s INTEGER,
  UNIQUE(vendor_uid,id)
);
-- Immutable upstream snapshot of every source epoch an application has used.
CREATE TABLE application_sources(
  app_uid TEXT NOT NULL REFERENCES applications(uid) ON DELETE CASCADE,
  epoch INTEGER NOT NULL CHECK(epoch>=1),
  provider TEXT NOT NULL CHECK(provider IN ('http-cache','codex','claude-code')),
  base_url TEXT NOT NULL,
  base_urls_json TEXT NOT NULL,
  source_strategy TEXT NOT NULL CHECK(source_strategy IN ('','ordered','round_robin','random')),
  created_at_s INTEGER NOT NULL,
  PRIMARY KEY(app_uid,epoch)
);

-- Configuration ----------------------------------------------------------------
-- Entity configuration is authoritative; the directory columns above are its
-- materialized projection.

CREATE TABLE template_snapshots(
  kind TEXT NOT NULL CHECK(kind IN ('Vendor','App')),
  canonical_key TEXT NOT NULL,
  schema_version INTEGER NOT NULL CHECK(schema_version=1),
  metadata_json BLOB NOT NULL,
  spec_json BLOB NOT NULL,
  semantic_hash TEXT NOT NULL CHECK(length(semantic_hash)=64),
  present INTEGER NOT NULL CHECK(present IN (0,1)),
  PRIMARY KEY(kind,canonical_key)
);
-- Trusted build contracts come only from embedded presets and are kept apart
-- from user-editable configuration.
CREATE TABLE trusted_distribution_snapshots(
  kind TEXT NOT NULL CHECK(kind='App'),
  canonical_key TEXT PRIMARY KEY,
  provider TEXT NOT NULL CHECK(provider IN ('codex','claude-code')),
  descriptor_json BLOB NOT NULL,
  distribution_digest TEXT NOT NULL CHECK(length(distribution_digest)=64),
  FOREIGN KEY(kind,canonical_key) REFERENCES template_snapshots(kind,canonical_key) ON DELETE CASCADE
);
CREATE TABLE vendor_config(
  entity_uid TEXT PRIMARY KEY REFERENCES vendors(uid) ON DELETE CASCADE,
  template_ref TEXT,
  overrides_json BLOB NOT NULL,
  spec_json BLOB,
  CHECK((template_ref IS NOT NULL AND spec_json IS NULL) OR (template_ref IS NULL AND spec_json IS NOT NULL))
);
CREATE TABLE application_config(
  entity_uid TEXT PRIMARY KEY REFERENCES applications(uid) ON DELETE CASCADE,
  template_ref TEXT,
  overrides_json BLOB NOT NULL,
  spec_json BLOB,
  CHECK((template_ref IS NOT NULL AND spec_json IS NULL) OR (template_ref IS NULL AND spec_json IS NOT NULL))
);
CREATE TABLE application_instructions(
  app_uid TEXT PRIMARY KEY REFERENCES applications(uid) ON DELETE CASCADE,
  revision INTEGER NOT NULL CHECK(revision>=1),
  en TEXT NOT NULL,
  zh_cn TEXT NOT NULL
);
-- Normalized HTTP cache policy of an http-cache application, projected from its
-- configuration.
CREATE TABLE application_http_policies(
  app_uid TEXT PRIMARY KEY REFERENCES applications(uid) ON DELETE CASCADE,
  payload BLOB NOT NULL
);
-- Private administrator notes never belong to public records or templates.
CREATE TABLE vendor_admin_notes(
  entity_uid TEXT PRIMARY KEY REFERENCES vendors(uid) ON DELETE CASCADE,
  revision INTEGER NOT NULL CHECK(revision>=1),
  text TEXT NOT NULL
);
CREATE TABLE application_admin_notes(
  entity_uid TEXT PRIMARY KEY REFERENCES applications(uid) ON DELETE CASCADE,
  revision INTEGER NOT NULL CHECK(revision>=1),
  text TEXT NOT NULL
);
-- A deletion intent survives a crash so startup can finish it.
CREATE TABLE pending_application_deletes(
  app_uid TEXT PRIMARY KEY REFERENCES applications(uid) ON DELETE CASCADE,
  requested_revision INTEGER NOT NULL
);
CREATE TABLE configuration_import_receipts(
  id TEXT PRIMARY KEY,
  result_json BLOB NOT NULL,
  created_s INTEGER NOT NULL
);
-- Global settings; their payloads are typed by internal/site and internal/config
-- (site, public_url) and by the configuration state (upstream_proxy).
CREATE TABLE settings(
  key TEXT PRIMARY KEY CHECK(key IN ('site','upstream_proxy','public_url')),
  revision INTEGER NOT NULL CHECK(revision>=1),
  payload BLOB NOT NULL
);

-- Taxonomy -------------------------------------------------------------------
-- Categories are deleted only when no application or template references them,
-- so the category references below do not cascade.

CREATE TABLE categories(
  id TEXT PRIMARY KEY,
  name_en TEXT NOT NULL,
  name_zh_cn TEXT NOT NULL,
  revision INTEGER NOT NULL,
  builtin INTEGER NOT NULL,
  present INTEGER NOT NULL,
  default_en TEXT,
  default_zh_cn TEXT,
  override_en TEXT,
  override_zh_cn TEXT
);
CREATE TABLE category_state(
  id INTEGER PRIMARY KEY CHECK(id=1),
  public_revision INTEGER NOT NULL
);
CREATE TABLE application_categories(
  app_uid TEXT NOT NULL REFERENCES applications(uid) ON DELETE CASCADE,
  category_id TEXT NOT NULL REFERENCES categories(id),
  PRIMARY KEY(app_uid,category_id)
);
CREATE INDEX application_category_filter ON application_categories(category_id,app_uid);
CREATE TABLE application_tags(
  app_uid TEXT NOT NULL REFERENCES applications(uid) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL,
  tag TEXT NOT NULL,
  folded TEXT NOT NULL,
  PRIMARY KEY(app_uid,folded),
  UNIQUE(app_uid,ordinal)
);
CREATE TABLE template_category_refs(
  template_key TEXT NOT NULL,
  category_id TEXT NOT NULL REFERENCES categories(id),
  PRIMARY KEY(template_key,category_id)
);
CREATE INDEX template_category_usage ON template_category_refs(category_id,template_key);

-- Public catalog -------------------------------------------------------------

CREATE TABLE catalog_state(
  id INTEGER PRIMARY KEY CHECK(id=1),
  revision INTEGER NOT NULL CHECK(revision>=0),
  ranking_salt BLOB NOT NULL CHECK(length(ranking_salt)=32)
);
CREATE TABLE homepage_pins(
  app_uid TEXT PRIMARY KEY REFERENCES applications(uid) ON DELETE CASCADE,
  position INTEGER NOT NULL UNIQUE CHECK(position>=0)
);
-- Hourly HLL registers merge to estimate distinct download clients over seven days.
CREATE TABLE download_sketches(
  app_uid TEXT NOT NULL REFERENCES applications(uid) ON DELETE CASCADE,
  hour_s INTEGER NOT NULL,
  registers BLOB NOT NULL CHECK(length(registers)=1024),
  PRIMARY KEY(app_uid,hour_s)
);

-- Release storage ------------------------------------------------------------
-- A version owns its metadata, channels and resources; a resource owns its
-- download generations. Deleting a version removes all of them.

CREATE TABLE app_versions(
  app_id TEXT NOT NULL,
  version TEXT NOT NULL,
  first_seen_s INTEGER NOT NULL,
  artifact_requests INTEGER NOT NULL DEFAULT 0 CHECK(artifact_requests>=0),
  downstream_bytes INTEGER NOT NULL DEFAULT 0 CHECK(downstream_bytes>=0),
  PRIMARY KEY(app_id,version)
);
CREATE TABLE release_metadata(
  app_id TEXT NOT NULL,
  version TEXT NOT NULL,
  raw BLOB NOT NULL,
  signature BLOB,
  trust_revision INTEGER NOT NULL CHECK(trust_revision>=1),
  fetched_at_s INTEGER NOT NULL,
  PRIMARY KEY(app_id,version),
  FOREIGN KEY(app_id,version) REFERENCES app_versions(app_id,version) ON DELETE CASCADE
);
CREATE TABLE channels(
  app_id TEXT NOT NULL,
  channel TEXT NOT NULL,
  version TEXT NOT NULL,
  fetched_at_s INTEGER NOT NULL,
  expires_at_s INTEGER NOT NULL,
  PRIMARY KEY(app_id,channel),
  FOREIGN KEY(app_id,version) REFERENCES release_metadata(app_id,version) ON DELETE CASCADE
);
CREATE TABLE resources(
  app_id TEXT NOT NULL,
  version TEXT NOT NULL,
  resource_key TEXT NOT NULL,
  source_url TEXT NOT NULL,
  sha256 TEXT NOT NULL CHECK(length(sha256)=64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
  expected_size INTEGER CHECK(expected_size>=0),
  PRIMARY KEY(app_id,version,resource_key),
  UNIQUE(app_id,version,resource_key,sha256),
  FOREIGN KEY(app_id,version) REFERENCES release_metadata(app_id,version) ON DELETE CASCADE
);
CREATE TABLE blobs(
  app_id TEXT NOT NULL,
  sha256 TEXT NOT NULL CHECK(length(sha256)=64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
  size_bytes INTEGER NOT NULL CHECK(size_bytes>=0),
  verified_at_s INTEGER NOT NULL,
  PRIMARY KEY(app_id,sha256)
);
-- A blob is deleted only once no generation references it, so the blob
-- reference deliberately does not cascade.
CREATE TABLE generations(
  id TEXT PRIMARY KEY,
  app_id TEXT NOT NULL,
  version TEXT NOT NULL,
  resource_key TEXT NOT NULL,
  expected_sha256 TEXT NOT NULL,
  blob_sha256 TEXT,
  phase TEXT NOT NULL CHECK(phase IN ('incomplete','complete','failed')),
  is_current INTEGER NOT NULL CHECK(is_current IN (0,1)),
  retired_at_s INTEGER,
  bytes INTEGER NOT NULL CHECK(bytes>=0),
  total_bytes INTEGER CHECK(total_bytes>=0),
  source_bytes INTEGER NOT NULL CHECK(source_bytes>=0),
  etag TEXT,
  resumes INTEGER NOT NULL CHECK(resumes>=0),
  full_retry INTEGER NOT NULL CHECK(full_retry IN (0,1)),
  started_at_s INTEGER NOT NULL,
  finished_at_s INTEGER,
  download_ns INTEGER NOT NULL CHECK(download_ns>=0),
  verification_ns INTEGER CHECK(verification_ns>=0),
  last_error_code TEXT,
  app_revision INTEGER NOT NULL CHECK(app_revision>=0),
  vendor_revision INTEGER NOT NULL CHECK(vendor_revision>=0),
  CHECK(is_current=0 OR retired_at_s IS NULL),
  CHECK(blob_sha256 IS NULL OR blob_sha256=expected_sha256),
  CHECK((phase='complete' AND blob_sha256 IS NOT NULL) OR (phase<>'complete' AND blob_sha256 IS NULL)),
  FOREIGN KEY(app_id,version,resource_key,expected_sha256)
    REFERENCES resources(app_id,version,resource_key,sha256) ON DELETE CASCADE,
  FOREIGN KEY(app_id,blob_sha256) REFERENCES blobs(app_id,sha256)
);
CREATE UNIQUE INDEX generations_current ON generations(app_id,version,resource_key) WHERE is_current=1;
CREATE INDEX generations_blob ON generations(app_id,blob_sha256);
CREATE TABLE cleanup_previews(
  id TEXT PRIMARY KEY,
  app_id TEXT NOT NULL,
  app_revision INTEGER NOT NULL CHECK(app_revision>=0),
  vendor_revision INTEGER NOT NULL CHECK(vendor_revision>=0),
  created_at_s INTEGER NOT NULL,
  expires_at_s INTEGER NOT NULL,
  selection_json BLOB NOT NULL,
  retention_json BLOB,
  executed_at_s INTEGER,
  result_json BLOB
);
CREATE TABLE retention_status(
  app_uid TEXT PRIMARY KEY REFERENCES applications(uid) ON DELETE CASCADE,
  payload BLOB NOT NULL
);

-- HTTP cache -----------------------------------------------------------------
-- Mutable HTTP representations have observed hashes, never release authorizations.

CREATE TABLE http_cache_generations(
  row_no INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT NOT NULL UNIQUE CHECK(length(id)=32 AND id NOT GLOB '*[^0-9a-f]*'),
  storage_id TEXT NOT NULL,
  path TEXT NOT NULL,
  source_url TEXT NOT NULL DEFAULT '',
  sha256 TEXT NOT NULL CHECK(length(sha256)=64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
  size_bytes INTEGER NOT NULL CHECK(size_bytes>=0),
  headers_json BLOB NOT NULL,
  fetched_at_s INTEGER NOT NULL,
  validated_at_s INTEGER NOT NULL,
  fresh_until_s INTEGER NOT NULL,
  last_access_bucket_s INTEGER NOT NULL DEFAULT 0,
  is_current INTEGER NOT NULL CHECK(is_current IN (0,1)),
  retired_at_s INTEGER
);
CREATE UNIQUE INDEX http_cache_current ON http_cache_generations(storage_id,path) WHERE is_current=1;
CREATE INDEX http_cache_scan ON http_cache_generations(storage_id,row_no) WHERE is_current=1;
CREATE TABLE http_cleanup_previews(
  id TEXT PRIMARY KEY,
  storage_id TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'cleanup' CHECK(kind IN ('cleanup','refresh')),
  state TEXT NOT NULL DEFAULT 'ready' CHECK(state IN ('building','ready','running','done','failed')),
  app_revision INTEGER NOT NULL,
  vendor_revision INTEGER NOT NULL,
  created_at_s INTEGER NOT NULL,
  expires_at_s INTEGER NOT NULL,
  high_water INTEGER NOT NULL DEFAULT 0 CHECK(high_water>=0),
  scanned_count INTEGER NOT NULL DEFAULT 0 CHECK(scanned_count>=0),
  selected_count INTEGER NOT NULL DEFAULT 0 CHECK(selected_count>=0),
  completed_count INTEGER NOT NULL DEFAULT 0 CHECK(completed_count>=0),
  failed_count INTEGER NOT NULL DEFAULT 0 CHECK(failed_count>=0),
  selected_bytes INTEGER NOT NULL DEFAULT 0 CHECK(selected_bytes>=0),
  selection_json BLOB NOT NULL,
  retention_json BLOB,
  executed_at_s INTEGER,
  result_json BLOB
);
CREATE TABLE http_cleanup_preview_items(
  preview_id TEXT NOT NULL REFERENCES http_cleanup_previews(id) ON DELETE CASCADE,
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
  PRIMARY KEY(preview_id,ordinal)
);
CREATE INDEX http_cleanup_preview_pending ON http_cleanup_preview_items(preview_id,result_status,ordinal);

-- Hosted files ---------------------------------------------------------------

CREATE TABLE hosted_files(
  app_uid TEXT NOT NULL REFERENCES applications(uid) ON DELETE CASCADE,
  path TEXT NOT NULL,
  id TEXT NOT NULL UNIQUE,
  sha256 TEXT NOT NULL,
  size_bytes INTEGER NOT NULL CHECK(size_bytes>=0),
  created_at_s INTEGER NOT NULL,
  PRIMARY KEY(app_uid,path)
);

-- Prewarm --------------------------------------------------------------------

CREATE TABLE prewarm_jobs(
  id TEXT PRIMARY KEY,
  app_uid TEXT NOT NULL REFERENCES applications(uid) ON DELETE CASCADE,
  storage_id TEXT NOT NULL,
  request_id TEXT NOT NULL,
  automatic INTEGER NOT NULL,
  fingerprint TEXT NOT NULL,
  policy_hash TEXT NOT NULL,
  success_fingerprint TEXT NOT NULL,
  resolved_version TEXT NOT NULL,
  input_json BLOB NOT NULL,
  state TEXT NOT NULL,
  reason TEXT NOT NULL,
  completed INTEGER NOT NULL,
  succeeded INTEGER NOT NULL,
  read_bytes INTEGER NOT NULL,
  ignored_json BLOB NOT NULL,
  app_revision INTEGER NOT NULL,
  vendor_revision INTEGER NOT NULL,
  created_s INTEGER NOT NULL,
  updated_s INTEGER NOT NULL,
  UNIQUE(app_uid,request_id)
);
CREATE UNIQUE INDEX prewarm_one_running ON prewarm_jobs((1)) WHERE state='running';
CREATE TABLE prewarm_items(
  job_id TEXT NOT NULL REFERENCES prewarm_jobs(id) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL,
  item_key TEXT NOT NULL,
  status TEXT NOT NULL,
  reason TEXT NOT NULL,
  read_bytes INTEGER NOT NULL,
  PRIMARY KEY(job_id,ordinal)
);
CREATE TABLE prewarm_success(
  app_uid TEXT NOT NULL REFERENCES applications(uid) ON DELETE CASCADE,
  channel TEXT NOT NULL,
  fingerprint TEXT NOT NULL,
  PRIMARY KEY(app_uid,channel)
);

-- Metrics and events ---------------------------------------------------------

CREATE TABLE metric_counters(
  scope TEXT NOT NULL,
  app_id TEXT NOT NULL,
  metric TEXT NOT NULL,
  value INTEGER NOT NULL CHECK(value>=0),
  observed_since_s INTEGER NOT NULL,
  PRIMARY KEY(scope,app_id,metric),
  CHECK((scope='global' AND app_id='') OR (scope='app' AND app_id<>''))
);
CREATE TABLE metric_samples(
  scope TEXT NOT NULL,
  app_id TEXT NOT NULL,
  metric TEXT NOT NULL,
  t_s INTEGER NOT NULL,
  observed_at_s INTEGER NOT NULL,
  boot TEXT NOT NULL,
  value REAL NOT NULL,
  delta REAL,
  duration_s REAL NOT NULL,
  PRIMARY KEY(scope,app_id,metric,t_s),
  CHECK((scope='global' AND app_id='') OR (scope='app' AND app_id<>''))
);
CREATE INDEX metric_samples_time ON metric_samples(t_s);
CREATE TABLE metric_hours(
  scope TEXT NOT NULL,
  app_id TEXT NOT NULL,
  metric TEXT NOT NULL,
  t_s INTEGER NOT NULL,
  min REAL,
  max REAL,
  avg REAL,
  last REAL,
  count INTEGER NOT NULL,
  delta REAL,
  delta_count INTEGER NOT NULL,
  duration_s REAL NOT NULL,
  PRIMARY KEY(scope,app_id,metric,t_s),
  CHECK((scope='global' AND app_id='') OR (scope='app' AND app_id<>''))
);
CREATE INDEX metric_hours_time ON metric_hours(t_s);
CREATE TABLE metric_history_state(
  id INTEGER PRIMARY KEY CHECK(id=1),
  aggregated_before_s INTEGER NOT NULL
);
CREATE TABLE events(
  id INTEGER PRIMARY KEY,
  time_s INTEGER NOT NULL,
  app_id TEXT,
  version TEXT,
  resource_key TEXT,
  generation_id TEXT,
  category TEXT NOT NULL,
  code TEXT NOT NULL,
  message TEXT NOT NULL,
  upstream_status INTEGER
);
CREATE INDEX events_time ON events(time_s);
CREATE INDEX events_app ON events(app_id,id);

-- Administration -------------------------------------------------------------

CREATE TABLE admin(
  id INTEGER PRIMARY KEY CHECK(id=1),
  hash BLOB NOT NULL,
  revision INTEGER NOT NULL
);
-- Transactional deletion receipts make physical cleanup restartable after a crash.
CREATE TABLE pending_object_deletes(
  path TEXT PRIMARY KEY
);

INSERT INTO category_state(id,public_revision) VALUES(1,1);
INSERT INTO catalog_state(id,revision,ranking_salt) VALUES(1,1,randomblob(32));
-- Settings documents exist from creation so that every editable revision is at least 1.
INSERT INTO settings(key,revision,payload) VALUES('site',1,'{}'),('public_url',1,'{"override_url":null}'),('upstream_proxy',1,'{"mode":"direct"}');
INSERT INTO metric_history_state(id,aggregated_before_s) VALUES(1,0);
