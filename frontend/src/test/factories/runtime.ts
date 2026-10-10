/*
 * Fixtures for the application runtime pages (package C): releases,
 * retention, prewarm, HTTP cache and hosted files.
 */
import type { Schema } from "@/shared/api";
import { localized } from "./index";

type Overrides<T> = Partial<T>;

const UID = "0123456789abcdef0123456789abcdef";
const SHA = "a".repeat(64);
const CREATED = "2026-10-01T08:00:00Z";

export function hexId(seed: number): string {
  return seed.toString(16).padStart(32, "0");
}

export function adminApp(overrides: Overrides<Schema<"App">> = {}): Schema<"App"> {
  return {
    uid: UID,
    id: "codex",
    key: "openai/codex",
    vendor_uid: hexId(1),
    vendor_id: "openai",
    name: localized("Codex CLI"),
    description: localized("OpenAI’s coding agent for your terminal."),
    icon: "/assets/presets/openai/codex/icon.svg",
    provider: "codex",
    base_url: "https://releases.openai.com/codex",
    cache_ttl_seconds: 60,
    categories: [],
    tags: [],
    enabled: true,
    builtin_template: true,
    source_epoch: 2,
    revision: 7,
    deleted_at: null,
    ...overrides,
  };
}

export function httpCacheApp(overrides: Overrides<Schema<"App">> = {}): Schema<"App"> {
  const { base_url: _unused, ...app } = adminApp({
    id: "mirror",
    key: "example/mirror",
    vendor_id: "example",
    provider: "http-cache",
    builtin_template: false,
  });
  return {
    ...app,
    base_urls: ["https://origin.example.com"],
    source_strategy: "ordered",
    ...overrides,
  };
}

export function metric(overrides: Overrides<Schema<"Metric">> = {}): Schema<"Metric"> {
  return {
    key: "versions.total",
    label: "Versions",
    kind: "gauge",
    unit: "count",
    group: "resources",
    value: 3,
    observed_seconds: 0,
    ...overrides,
  };
}

export function appStatus(overrides: Overrides<Schema<"AppStatus">> = {}): Schema<"AppStatus"> {
  return {
    sampled_at: "2026-10-10T08:00:00Z",
    metrics: [
      metric(),
      metric({ key: "resources.complete", label: "Complete", value: 12 }),
      metric({
        key: "counters.downstream_bytes",
        label: "Downstream bytes",
        kind: "counter",
        unit: "bytes",
        group: "traffic",
        value: 1024 * 1024 * 5,
      }),
      metric({ key: "resources.failed", label: "Failed", value: 1 }),
    ],
    ...overrides,
  };
}

export function historySeries(
  overrides: Overrides<Schema<"HistorySeries">> = {},
): Schema<"HistorySeries"> {
  return {
    key: "versions.total",
    label: "Versions",
    kind: "gauge",
    unit: "count",
    group: "resources",
    scope: "app",
    app_key: "openai/codex",
    range: "24h",
    resolution_seconds: 60,
    from: "2026-10-09T08:00:00Z",
    to: "2026-10-10T08:00:00Z",
    points: [
      {
        time: "2026-10-10T07:59:00Z",
        value: 3,
        min: 3,
        max: 3,
        avg: 3,
        last: 3,
        count: 1,
        delta: null,
        delta_count: 0,
        observed_seconds: 0,
        partial: false,
        incomplete: false,
      },
    ],
    ...overrides,
  };
}

export function version(overrides: Overrides<Schema<"Version">> = {}): Schema<"Version"> {
  return {
    version: "0.46.0",
    first_seen: CREATED,
    requests: 42,
    downstream_bytes: 1024 * 1024 * 80,
    ...overrides,
  };
}

export function resource(overrides: Overrides<Schema<"Resource">> = {}): Schema<"Resource"> {
  return {
    id: "gen-0001",
    version: "0.46.0",
    key: "linux-x64/codex",
    state: "complete",
    current: true,
    retired: false,
    bytes: 1024 * 1024 * 40,
    total_bytes: 1024 * 1024 * 40,
    expected_bytes: 1024 * 1024 * 40,
    sha256: SHA,
    readers: 0,
    active_writer: false,
    recent_bps: 0,
    average_bps: 1024 * 1024,
    resumes: 0,
    verification_ns: 1_000_000,
    download_ns: 40_000_000_000,
    started_at: CREATED,
    finished_at: "2026-10-01T08:01:00Z",
    error: null,
    ...overrides,
  };
}

export function sourceEpoch(
  overrides: Overrides<Schema<"SourceEpoch">> = {},
): Schema<"SourceEpoch"> {
  return {
    epoch: 2,
    base_url: "https://releases.openai.com/codex",
    current: true,
    active: true,
    created_at: CREATED,
    ...overrides,
  };
}

export function versionCleanupPreview(
  overrides: Overrides<Schema<"VersionCleanupPreview">> = {},
): Schema<"VersionCleanupPreview"> {
  return {
    id: hexId(100),
    source_epoch: 2,
    created_at: new Date().toISOString(),
    expires_at: new Date(Date.now() + 10 * 60_000).toISOString(),
    executed_at: null,
    selected: [
      {
        generation_id: "gen-0042",
        version: "0.40.0",
        resource_key: "linux-x64/codex",
        bytes: 1024 * 1024 * 30,
      },
    ],
    logical_bytes: 1024 * 1024 * 30,
    reclaimable_bytes: 1024 * 1024 * 20,
    active_generations: 0,
    unknown_versions: [],
    ...overrides,
  };
}

/** A template-linked release application configuration. */
export function appConfiguration(
  overrides: Overrides<Schema<"AppConfiguration">> = {},
): Schema<"AppConfiguration"> {
  const spec: Schema<"AppSpec"> = {
    name: localized("Codex CLI"),
    description: localized("OpenAI’s coding agent for your terminal."),
    icon: "/assets/presets/openai/codex/icon.svg",
    provider: "codex",
    proxy: { mode: "inherit" },
    base_url: "https://releases.openai.com/codex",
    cache_ttl_seconds: 60,
    categories: [],
    tags: [],
    instructions: { en: "", "zh-CN": "" },
    retention: { enabled: false, keep_latest: 3 },
    prewarm: { enabled: false, channels: [], platforms: [] },
  };
  return {
    revision: 7,
    template_ref: "openai/codex",
    template_hash: "3c9d",
    template_missing: false,
    defaults: spec,
    overrides: {},
    effective: structuredClone(spec),
    fields: {
      cache_ttl_seconds: { source: "inherited", differs_from_template: false },
      retention: { source: "inherited", differs_from_template: false },
      prewarm: { source: "inherited", differs_from_template: false },
    },
    proxy_effective: { mode: "direct", source_scope: "global", source_id: "", dns: "local" },
    ...overrides,
  };
}

/** An HTTP cache application configuration without template. */
export function httpCacheConfiguration(
  policy: Partial<Schema<"HttpPolicy">> = {},
  overrides: Overrides<Schema<"AppConfiguration">> = {},
): Schema<"AppConfiguration"> {
  const spec: Schema<"AppSpec"> = {
    name: localized("Mirror"),
    description: localized(""),
    icon: "/assets/presets/generic/icon.svg",
    provider: "http-cache",
    proxy: { mode: "inherit" },
    base_urls: ["https://origin.example.com"],
    source_strategy: "ordered",
    cache_ttl_seconds: 300,
    categories: [],
    tags: [],
    instructions: { en: "", "zh-CN": "" },
    http_policy: { stale_fallback: true, rules: [], auto_cleanup: [], ...policy },
  };
  return appConfiguration({
    template_ref: null,
    template_hash: null,
    defaults: null,
    effective: spec,
    fields: {},
    ...overrides,
  });
}

export function retentionStatus(
  overrides: Overrides<Schema<"RetentionStatus">> = {},
): Schema<"RetentionStatus"> {
  return {
    last_run: {
      attempted_at: "2026-10-10T07:00:00Z",
      succeeded_at: "2026-10-10T07:00:00Z",
      outcome: "success",
      reason: null,
      retired_versions: 2,
      logical_bytes: 1024 * 1024 * 60,
    },
    next_check_at: "2026-10-10T09:00:00Z",
    ...overrides,
  };
}

export function retentionPreview(
  overrides: Overrides<Schema<"RetentionPreview">> = {},
): Schema<"RetentionPreview"> {
  return {
    id: hexId(200),
    created_at: new Date().toISOString(),
    expires_at: new Date(Date.now() + 10 * 60_000).toISOString(),
    executed_at: null,
    selected_versions: 1,
    logical_bytes: 1024 * 1024 * 30,
    reclaimable_bytes: 1024 * 1024 * 30,
    result: null,
    ...overrides,
  };
}

export function retentionVersionPage(
  items: Schema<"RetentionVersion">[],
  overrides: Overrides<Schema<"RetentionVersionPage">> = {},
): Schema<"RetentionVersionPage"> {
  return { items, page: 1, limit: 25, total: items.length, total_pages: 1, ...overrides };
}

export function prewarmOptions(
  overrides: Overrides<Schema<"PrewarmOptions">> = {},
): Schema<"PrewarmOptions"> {
  return {
    kind: "release",
    channels: ["latest", "stable"],
    platforms: [
      { id: "linux-x64", name: "Linux x64" },
      { id: "darwin-arm64", name: "macOS Apple silicon" },
    ],
    default_limits: {
      max_files: 10000,
      max_depth: 16,
      max_download_bytes: 10 * 1024 ** 3,
      max_duration_seconds: 3600,
    },
    ...overrides,
  };
}

export function prewarmJob(overrides: Overrides<Schema<"PrewarmJob">> = {}): Schema<"PrewarmJob"> {
  return {
    id: hexId(300),
    state: "running",
    reason: null,
    automatic: false,
    target: "latest",
    resolved_version: "0.46.0",
    platforms: ["linux-x64"],
    created_at: CREATED,
    updated_at: CREATED,
    completed: 1,
    succeeded: 1,
    bytes: 1024 * 1024,
    ignored: {},
    limits: prewarmOptions().default_limits,
    ...overrides,
  };
}

export function prewarmItemPage(
  items: Schema<"PrewarmItem">[],
  overrides: Overrides<Schema<"PrewarmItemPage">> = {},
): Schema<"PrewarmItemPage"> {
  return { items, page: 1, limit: 25, total: items.length, total_pages: 1, ...overrides };
}

export function cacheEntry(overrides: Overrides<Schema<"CacheEntry">> = {}): Schema<"CacheEntry"> {
  return {
    generation_id: "gen-a",
    path: "/releases/1.2.3/tool.zip",
    size_bytes: 2048,
    sha256: SHA,
    etag: '"abc"',
    source_url: "https://origin.example.com/releases/1.2.3/tool.zip",
    fetched_at: CREATED,
    validated_at: CREATED,
    last_access_at: null,
    fresh_until: "2099-01-01T00:00:00Z",
    ...overrides,
  };
}

export function maintenancePreview(
  overrides: Overrides<Schema<"MaintenancePreview">> = {},
): Schema<"MaintenancePreview"> {
  return {
    id: hexId(400),
    kind: "cleanup",
    state: "ready",
    source_epoch: 2,
    match: { type: "glob", pattern: "/" },
    basis: "last_access",
    before: "2026-09-01T00:00:00Z",
    created_at: new Date().toISOString(),
    expires_at: new Date(Date.now() + 10 * 60_000).toISOString(),
    scanned_files: 10,
    selected_files: 2,
    selected_bytes: 4096,
    active_files: 0,
    completed_files: 0,
    failed_files: 0,
    result: null,
    ...overrides,
  };
}

export function maintenanceItemPage(
  items: Schema<"MaintenanceItem">[],
  overrides: Overrides<Schema<"MaintenanceItemPage">> = {},
): Schema<"MaintenanceItemPage"> {
  return {
    items,
    next_cursor: null,
    total_files: items.length,
    total_bytes: items.reduce((sum, item) => sum + item.size_bytes, 0),
    state: "ready",
    ...overrides,
  };
}

export function maintenanceItem(
  overrides: Overrides<Schema<"MaintenanceItem">> = {},
): Schema<"MaintenanceItem"> {
  return {
    ordinal: 0,
    generation_id: "gen-a",
    path: "/releases/1.2.3/tool.zip",
    size_bytes: 2048,
    result_status: "pending",
    error_code: null,
    ...overrides,
  };
}

export function autoCleanupStatus(
  overrides: Overrides<Schema<"AutoCleanupStatus">> = {},
): Schema<"AutoCleanupStatus"> {
  return {
    running: false,
    interval_seconds: 900,
    scan_limit_per_app: 1000,
    retire_limit_per_app: 100,
    last_attempt_at: "2026-10-10T07:45:00Z",
    last_success_at: "2026-10-10T07:45:00Z",
    last_error_at: null,
    last_error: null,
    passes_total: 12,
    failures_total: 0,
    configured_apps: 3,
    scanned_files: 120,
    retired_files: 4,
    skipped_accessed: 1,
    skipped_changed: 0,
    retired_bytes: 8192,
    ...overrides,
  };
}

export function hostedFile(overrides: Overrides<Schema<"HostedFile">> = {}): Schema<"HostedFile"> {
  return {
    id: hexId(500),
    path: "tools/setup.exe",
    sha256: SHA,
    size_bytes: 4096,
    created_at: CREATED,
    ...overrides,
  };
}

export function hostedFilePage(
  items: Schema<"HostedFile">[],
  overrides: Overrides<Schema<"HostedFilePage">> = {},
): Schema<"HostedFilePage"> {
  return { items, page: 1, limit: 25, total: items.length, total_pages: 1, ...overrides };
}
