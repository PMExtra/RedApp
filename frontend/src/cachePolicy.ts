export interface MatcherSpec {
  type: "glob" | "re2";
  pattern: string;
}
export interface CacheTTLRule {
  id?: string;
  match: MatcherSpec;
  ttl_seconds: number;
}
export interface AutoCleanupRule {
  match: MatcherSpec;
  basis: "fetched_at" | "last_access";
  age_seconds: number;
}
export interface CachePolicy {
  stale_fallback: boolean;
  rules: CacheTTLRule[];
  auto_cleanup: AutoCleanupRule[];
}
export interface AutoCleanupStatus {
  running: boolean;
  interval_seconds: number;
  scan_limit_per_app: number;
  retire_limit_per_app: number;
  last_attempt_at: string | null;
  last_success_at: string | null;
  last_error_at: string | null;
  last_error: string;
  passes_total: number;
  failures_total: number;
  configured_apps: number;
  scanned_files: number;
  retired_files: number;
  skipped_accessed: number;
  skipped_changed: number;
  retired_bytes: number;
}
