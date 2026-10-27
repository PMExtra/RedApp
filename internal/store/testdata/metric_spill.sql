-- Synthetic metrics only. Five days of hourly rows scatter the pages changed
-- by the real hourly UPSERT enough to spill SQLite's statement subjournal.
-- No application configuration or credentials are included.
DELETE FROM metric_samples;
DELETE FROM metric_hours;
CREATE TABLE fixture_metric_definitions(metric,kind,per_app);
INSERT INTO fixture_metric_definitions VALUES
('disk.used_bytes', 'gauge', 0),
('disk.allocated_cache_bytes', 'gauge', 0),
('disk.allocated_temporary_bytes', 'gauge', 0),
('disk.allocated_pending_bytes', 'gauge', 0),
('disk.other_bytes', 'gauge', 0),
('disk.free_bytes', 'gauge', 0),
('counters.requests', 'counter', 0),
('counters.artifact_requests', 'counter', 1),
('counters.cache_hit_requests', 'counter', 1),
('counters.shared_follower_requests', 'counter', 1),
('counters.miss_requests', 'counter', 1),
('counters.download_success', 'counter', 1),
('counters.download_errors', 'counter', 1),
('counters.upstream_errors', 'counter', 1),
('counters.upstream_bytes', 'counter', 1),
('counters.downstream_bytes', 'counter', 1),
('counters.cleanup_freed_bytes', 'counter', 1),
('rates.upstream_bytes_per_second', 'rate', 0),
('rates.downstream_bytes_per_second', 'rate', 0),
('runtime.memory_bytes', 'gauge', 0),
('resources.current', 'gauge', 1),
('resources.retired', 'gauge', 1),
('resources.readers', 'gauge', 1),
('resources.active_writers', 'gauge', 1),
('resources.failed', 'gauge', 1),
('resources.invalid', 'gauge', 1),
('versions.total', 'gauge', 1);
CREATE TABLE fixture_metric_series(scope,app_id,metric,kind);
INSERT INTO fixture_metric_series SELECT 'global','',metric,kind FROM fixture_metric_definitions;
WITH RECURSIVE apps(i) AS (VALUES(0) UNION ALL SELECT i+1 FROM apps WHERE i<4)
INSERT INTO fixture_metric_series SELECT 'app','app/fixture'||i,metric,kind FROM apps CROSS JOIN fixture_metric_definitions WHERE per_app=1;
UPDATE metric_history_state SET aggregated_before_s=(CAST(strftime('%s','now') AS INTEGER)/3600)*3600-3600;
WITH RECURSIVE minutes(i) AS (VALUES(0) UNION ALL SELECT i+1 FROM minutes WHERE i<1440)
INSERT INTO metric_samples(scope,app_id,metric,t_s,observed_at_s,boot,value,delta,duration_s)
SELECT scope,app_id,metric,aggregated_before_s+3600-86460+i*60,aggregated_before_s+3600-86460+i*60,'fixture',42,1,CASE kind WHEN 'rate' THEN 5 WHEN 'counter' THEN 60 ELSE 0 END
FROM minutes CROSS JOIN fixture_metric_series CROSS JOIN metric_history_state ORDER BY i,metric,app_id;
WITH RECURSIVE hours(i) AS (VALUES(0) UNION ALL SELECT i+1 FROM hours WHERE i<118)
INSERT INTO metric_hours(scope,app_id,metric,t_s,min,max,avg,last,count,delta,delta_count,duration_s)
SELECT scope,app_id,metric,aggregated_before_s+3600-5*86400+i*3600,42,42,42,42,60,60,60,0
FROM hours CROSS JOIN fixture_metric_series CROSS JOIN metric_history_state ORDER BY i,metric,app_id;
DROP TABLE fixture_metric_definitions;
DROP TABLE fixture_metric_series;
