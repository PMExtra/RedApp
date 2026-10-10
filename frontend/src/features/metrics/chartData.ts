import type { HistoryPoint, HistorySeries } from "./catalog";

/** Counter charts show the cumulative value or the increase per bucket. */
export type CounterView = "value" | "delta";

export interface ChartLine {
  field: "value" | "delta" | "min" | "max";
  /** i18n key of the line name (`metrics.history.lines.*`). */
  labelKey: string;
  main: boolean;
}

/**
 * The lines drawn and listed in the readout. `value` is the raw sample (24h),
 * the bucket average (gauges, rates) or the last cumulative value (counters).
 */
export function chartLines(
  series: Pick<HistorySeries, "kind" | "resolution_seconds">,
  view: CounterView,
): ChartLine[] {
  if (series.kind === "counter") {
    return view === "delta"
      ? [{ field: "delta", labelKey: "metrics.history.lines.delta", main: true }]
      : [{ field: "value", labelKey: "metrics.history.lines.last", main: true }];
  }
  const main: ChartLine = {
    field: "value",
    labelKey:
      series.resolution_seconds === 60
        ? "metrics.history.lines.sample"
        : "metrics.history.lines.average",
    main: true,
  };
  if (series.resolution_seconds === 60) return [main];
  return [
    main,
    { field: "min", labelKey: "metrics.history.lines.min", main: false },
    { field: "max", labelKey: "metrics.history.lines.max", main: false },
  ];
}

export interface Timeline {
  /** Bucket starts in Unix seconds, one per resolution step. */
  times: number[];
  /** The point of each bucket, or `null` where the server has no data (a gap). */
  points: (HistoryPoint | null)[];
}

const MAX_BUCKETS = 50_000;

/**
 * Every bucket of the window, so missing samples become explicit gaps rather
 * than lines drawn across them. Points the grid does not cover are kept.
 */
export function buildTimeline(
  series: Pick<HistorySeries, "from" | "to" | "resolution_seconds" | "points">,
): Timeline {
  const step = series.resolution_seconds;
  const byTime = new Map<number, HistoryPoint>();
  for (const point of series.points) byTime.set(Date.parse(point.time) / 1000, point);
  const times = new Set(byTime.keys());
  const from = Date.parse(series.from) / 1000;
  const to = Date.parse(series.to) / 1000;
  if (Number.isFinite(from) && Number.isFinite(to) && step > 0) {
    const first = Math.floor(from / step) * step;
    for (let time = first; time < to && times.size < MAX_BUCKETS; time += step) times.add(time);
  }
  const sorted = [...times].sort((a, b) => a - b);
  return { times: sorted, points: sorted.map((time) => byTime.get(time) ?? null) };
}

/** uPlot's aligned data: x values, then one array per line (`null` = gap). */
export function alignedData(timeline: Timeline, lines: ChartLine[]): (number | null)[][] {
  return [
    timeline.times,
    ...lines.map((line) => timeline.points.map((point) => point?.[line.field] ?? null)),
  ];
}

export interface HistorySummary {
  latest: number | null;
  min: number | null;
  max: number | null;
  /** Buckets with at least one value of the main line. */
  covered: number;
  total: number;
}

export function summarize(timeline: Timeline, line: ChartLine): HistorySummary {
  let latest: number | null = null;
  let min: number | null = null;
  let max: number | null = null;
  let covered = 0;
  for (const point of timeline.points) {
    const value = point?.[line.field];
    if (value === null || value === undefined) continue;
    covered++;
    latest = value;
    min = min === null ? value : Math.min(min, value);
    max = max === null ? value : Math.max(max, value);
  }
  return { latest, min, max, covered, total: timeline.times.length };
}

/** Index of the last bucket with a value of `field`, else the last bucket. */
export function lastValueIndex(timeline: Timeline, field: ChartLine["field"]): number {
  for (let index = timeline.points.length - 1; index >= 0; index--) {
    const value = timeline.points[index]?.[field];
    if (value !== null && value !== undefined) return index;
  }
  return timeline.points.length - 1;
}
