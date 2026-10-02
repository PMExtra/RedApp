import type { HistoryPoint, HistorySeries, Metric } from "./api";
import { language, type Message } from "./i18n";

export interface HistoryLine {
  field: "value" | "delta" | "min" | "max";
  label: Message;
  color: string;
}

// Use the same descriptors for the plotted arrays and the point readout.
// The API's value is a minute observation, hourly avg, or counter last.
export function historyLines(
  series: Pick<HistorySeries, "kind" | "resolution_seconds">,
  mode: string,
): HistoryLine[] {
  const lines: HistoryLine[] = [
    {
      field: mode === "delta" && series.kind === "counter" ? "delta" : "value",
      label:
        series.kind === "counter"
          ? mode === "delta"
            ? "Observed increment (delta)"
            : "Last cumulative value (last)"
          : series.resolution_seconds === 60
            ? series.kind === "rate"
              ? "Five-second observation (value)"
              : "Observation (value)"
            : series.kind === "rate"
              ? "Observed weighted average (avg)"
              : "Observed average (avg)",
      color: "#146b56",
    },
  ];
  if (series.kind !== "counter")
    lines.push(
      { field: "min", label: "Observed minimum (min)", color: "#7598b4" },
      { field: "max", label: "Observed maximum (max)", color: "#a5773e" },
    );
  return lines;
}

export function historyData(points: HistoryPoint[], lines: HistoryLine[]) {
  return [
    points.map((point) => point.time),
    ...lines.map((line) => points.map((point) => point[line.field])),
  ];
}

// Show the exact number returned by the API, without the overview's rounding
// or IEC scaling. Hourly averages and rates can legitimately be fractional.
export function exactMetric(
  value: number | null | undefined,
  unit: Metric["unit"],
) {
  if (value == null || !Number.isFinite(value) || value < 0) return "—";
  const suffix = {
    bytes: " B",
    bytes_per_second: " B/s",
    seconds: " s",
    count: "",
  }[unit];
  return String(value) + suffix;
}

export function localBucketTime(seconds: number) {
  return new Intl.DateTimeFormat(language.value, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    timeZoneName: "shortOffset",
  }).format(new Date(seconds * 1000));
}
