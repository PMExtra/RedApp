import type { Schema } from "@/shared/api";

type Metric = Schema<"Metric">;
type MetricKey = Schema<"MetricKey">;

const GLOBAL_KEYS: MetricKey[] = [
  "disk.used_bytes",
  "disk.logical_bytes",
  "disk.allocated_cache_bytes",
  "disk.allocated_temporary_bytes",
  "disk.allocated_pending_bytes",
  "disk.cache_bytes",
  "disk.temporary_bytes",
  "disk.pending_bytes",
  "disk.other_bytes",
  "disk.free_bytes",
  "counters.requests",
  "counters.artifact_requests",
  "counters.cache_hit_requests",
  "counters.shared_follower_requests",
  "counters.miss_requests",
  "counters.download_success",
  "counters.download_errors",
  "counters.upstream_errors",
  "counters.upstream_bytes",
  "counters.downstream_bytes",
  "counters.cleanup_freed_bytes",
  "rates.upstream_bytes_per_second",
  "rates.downstream_bytes_per_second",
  "runtime.memory_bytes",
  "runtime.goroutines",
  "runtime.uptime_seconds",
  "resources.total",
  "resources.current",
  "resources.retired",
  "resources.readers",
  "resources.active_writers",
  "resources.queued",
  "resources.downloading",
  "resources.resuming",
  "resources.retry_wait",
  "resources.verifying",
  "resources.complete",
  "resources.failed",
  "resources.invalid",
  "resources.interrupted",
  "versions.total",
];

/** A catalog metric with the kind, unit and group the spec assigns to its key. */
export function metric(key: MetricKey, overrides: Partial<Metric> = {}): Metric {
  const [prefix] = key.split(".");
  const kind: Metric["kind"] =
    prefix === "counters" ? "counter" : prefix === "rates" ? "rate" : "gauge";
  const unit: Metric["unit"] =
    prefix === "rates"
      ? "bytes_per_second"
      : key.endsWith("_bytes")
        ? "bytes"
        : key === "runtime.uptime_seconds"
          ? "seconds"
          : "count";
  const group: Metric["group"] =
    prefix === "disk"
      ? "disk"
      : prefix === "counters"
        ? "traffic"
        : prefix === "rates"
          ? "speed"
          : prefix === "runtime"
            ? "runtime"
            : "resources";
  return {
    key,
    label: key,
    kind,
    unit,
    group,
    value: 1,
    observed_seconds: kind === "rate" ? 5 : 0,
    ...overrides,
  };
}

/** `getStatus` with all 41 metrics; `values` overrides single values. */
export function globalStatus(
  values: Partial<Record<MetricKey, number | null>> = {},
  overrides: Partial<Schema<"GlobalStatus">> = {},
): Schema<"GlobalStatus"> {
  return {
    sampled_at: "2026-10-10T12:00:00Z",
    started_at: "2026-10-01T08:00:00Z",
    metrics: GLOBAL_KEYS.map((key) =>
      metric(key, key in values ? { value: values[key] ?? null } : {}),
    ),
    ...overrides,
  };
}

/** `getAppStatus` with the 25 application metrics; `values` overrides single values. */
export function appStatus(
  values: Partial<Record<MetricKey, number | null>> = {},
  overrides: Partial<Schema<"AppStatus">> = {},
): Schema<"AppStatus"> {
  const keys = GLOBAL_KEYS.filter((key) => /^(counters|resources|versions)\./.test(key));
  return {
    sampled_at: "2026-10-10T12:00:00Z",
    metrics: keys.map((key) => metric(key, key in values ? { value: values[key] ?? null } : {})),
    ...overrides,
  };
}

export function historyPoint(
  time: string,
  value: number | null,
  overrides: Partial<Schema<"HistoryPoint">> = {},
): Schema<"HistoryPoint"> {
  return {
    time,
    value,
    min: value,
    max: value,
    avg: value,
    last: value,
    count: 60,
    delta: null,
    delta_count: 0,
    observed_seconds: 0,
    partial: false,
    incomplete: false,
    ...overrides,
  };
}

/** An hourly series from `from` with one point per given value (`null` = gap). */
export function historySeries(
  key: MetricKey,
  values: (number | null)[],
  overrides: Partial<Schema<"HistorySeries">> = {},
): Schema<"HistorySeries"> {
  const base = metric(key);
  const resolution = overrides.resolution_seconds ?? 3600;
  const from = Date.parse(overrides.from ?? "2026-10-10T00:00:00Z");
  const points = values.flatMap((value, index) =>
    value === null
      ? []
      : [historyPoint(new Date(from + index * resolution * 1000).toISOString(), value)],
  );
  return {
    key,
    label: base.label,
    kind: base.kind,
    unit: base.unit,
    group: base.group,
    scope: "global",
    app_key: null,
    range: "7d",
    resolution_seconds: 3600,
    from: new Date(from).toISOString(),
    to: new Date(from + values.length * resolution * 1000).toISOString(),
    points,
    ...overrides,
  };
}
