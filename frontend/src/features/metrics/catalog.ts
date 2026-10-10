import { useI18n } from "vue-i18n";
import type { Schema } from "@/shared/api";

export type Metric = Schema<"Metric">;
export type MetricKey = Schema<"MetricKey">;
export type MetricGroup = Metric["group"];
export type HistorySeries = Schema<"HistorySeries">;
export type HistoryPoint = Schema<"HistoryPoint">;
export type HistoryRange = HistorySeries["range"];

/** Display order of the spec's metric groups. */
export const METRIC_GROUPS: readonly MetricGroup[] = [
  "disk",
  "traffic",
  "speed",
  "runtime",
  "resources",
];

export const HISTORY_RANGES: readonly HistoryRange[] = ["24h", "7d", "30d"];
export const DEFAULT_HISTORY_RANGE: HistoryRange = "7d";

/**
 * The 16 global metrics shown first on the overview: capacity, downloads and
 * active transfers. The other 25 are diagnostic and start collapsed.
 */
export const GLOBAL_COMMON_METRICS: readonly MetricKey[] = [
  "disk.used_bytes",
  "disk.free_bytes",
  "disk.allocated_cache_bytes",
  "disk.allocated_temporary_bytes",
  "disk.allocated_pending_bytes",
  "counters.artifact_requests",
  "counters.download_success",
  "counters.download_errors",
  "counters.upstream_errors",
  "counters.upstream_bytes",
  "counters.downstream_bytes",
  "rates.upstream_bytes_per_second",
  "rates.downstream_bytes_per_second",
  "runtime.memory_bytes",
  "resources.readers",
  "resources.active_writers",
];

/** Common metrics of one application (`getAppStatus` has resources, counters and versions). */
export const APP_COMMON_METRICS: readonly MetricKey[] = [
  "versions.total",
  "counters.download_success",
  "counters.download_errors",
  "counters.upstream_bytes",
  "counters.downstream_bytes",
  "resources.readers",
  "resources.active_writers",
];

/**
 * Localized metric names. Every catalog key has a translation
 * (`metrics.labels.<key>`); the server's English label is a fallback for keys
 * added to the spec before the translations.
 */
export function useMetricLabels() {
  const i18n = useI18n();
  return {
    label: (metric: Pick<Metric, "key" | "label">) =>
      i18n.te(`metrics.labels.${metric.key}`)
        ? i18n.t(`metrics.labels.${metric.key}`)
        : metric.label,
    description: (key: MetricKey) =>
      i18n.te(`metrics.descriptions.${key}`) ? i18n.t(`metrics.descriptions.${key}`) : "",
    group: (group: MetricGroup) => i18n.t(`metrics.groups.${group}`),
  };
}
