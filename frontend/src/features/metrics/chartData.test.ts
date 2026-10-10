import { describe, expect, it } from "vitest";
import { historySeries } from "@/test/factories/metrics";
import { alignedData, buildTimeline, chartLines, summarize } from "./chartData";
import { formatMetricValue } from "./format";

describe("history timeline", () => {
  it("turns missing buckets into gaps instead of joining or zeroing them", () => {
    const series = historySeries("disk.used_bytes", [10, null, null, 40]);
    const timeline = buildTimeline(series);
    expect(timeline.times).toHaveLength(4);
    expect(timeline.points.map((point) => point?.value ?? null)).toEqual([10, null, null, 40]);

    const lines = chartLines(series, "value");
    expect(lines.map((line) => line.field)).toEqual(["value", "min", "max"]);
    expect(alignedData(timeline, lines)[1]).toEqual([10, null, null, 40]);
    const [main] = lines;
    if (!main) throw new Error("no main line");
    expect(summarize(timeline, main)).toEqual({
      latest: 40,
      min: 10,
      max: 40,
      covered: 2,
      total: 4,
    });
  });

  it("charts counters as running totals or increases, never averaged", () => {
    const series = historySeries("counters.download_success", [1, 2]);
    expect(chartLines(series, "value").map((line) => line.field)).toEqual(["value"]);
    expect(chartLines(series, "delta").map((line) => line.field)).toEqual(["delta"]);
  });
});

describe("metric values", () => {
  it.each([
    [1536, "bytes", "1.50 KiB"],
    [2048, "bytes_per_second", "2.00 KiB/s"],
    [0, "count", "0"],
    [null, "count", "—"],
    [90061, "seconds", "1 day 1 hr"],
  ] as const)("formats %s %s as %s", (value, unit, text) => {
    expect(formatMetricValue("en", value, unit)).toBe(text);
  });
});
