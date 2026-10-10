import { useI18n } from "vue-i18n";
import type { Schema } from "@/shared/api";
import { formatBytes, formatNumber, type Locale } from "@/shared/i18n";

export type MetricUnit = Schema<"Metric">["unit"];

const UNKNOWN = "—";
const DURATION_PARTS: [Intl.NumberFormatOptions["unit"], number][] = [
  ["day", 86400],
  ["hour", 3600],
  ["minute", 60],
  ["second", 1],
];

function finite(value: number | null | undefined): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

/** Uptime-style duration with the two largest units: "3 days 4 hr". */
function formatElapsed(locale: Locale, seconds: number): string {
  let rest = Math.floor(seconds);
  const parts: string[] = [];
  for (const [unit, size] of DURATION_PARTS) {
    const amount = Math.floor(rest / size);
    if (amount > 0 || (parts.length === 0 && unit === "second")) {
      parts.push(
        new Intl.NumberFormat(locale, { style: "unit", unit, unitDisplay: "short" }).format(amount),
      );
      rest -= amount * size;
    }
    if (parts.length === 2) break;
  }
  return parts.join(" ");
}

/**
 * A metric value for display: IEC bytes, bytes per second, counts and
 * durations. Unknown (`null`) and negative values show "—"; a measured zero
 * shows as zero.
 */
export function formatMetricValue(
  locale: Locale,
  value: number | null | undefined,
  unit: MetricUnit,
): string {
  if (!finite(value) || value < 0) return UNKNOWN;
  switch (unit) {
    case "bytes":
      return formatBytes(locale, value);
    case "bytes_per_second":
      return `${formatBytes(locale, value)}/s`;
    case "seconds":
      return formatElapsed(locale, value);
    default:
      return formatNumber(locale, value);
  }
}

const EXACT_SUFFIX: Record<MetricUnit, string> = {
  bytes: " B",
  bytes_per_second: " B/s",
  seconds: " s",
  count: "",
};

/** The exact number returned by the API (hourly averages can be fractional), with a base unit. */
export function formatExactMetricValue(
  locale: Locale,
  value: number | null | undefined,
  unit: MetricUnit,
): string {
  if (!finite(value) || value < 0) return UNKNOWN;
  return (
    new Intl.NumberFormat(locale, { maximumFractionDigits: 3 }).format(value) + EXACT_SUFFIX[unit]
  );
}

/** Metric formatters bound to the UI locale. */
export function useMetricFormat() {
  const { locale } = useI18n();
  return {
    value: (value: number | null | undefined, unit: MetricUnit) =>
      formatMetricValue(locale.value as Locale, value, unit),
    exact: (value: number | null | undefined, unit: MetricUnit) =>
      formatExactMetricValue(locale.value as Locale, value, unit),
  };
}
