import { afterEach, describe, expect, it, vi } from "vitest";
import {
  exactMetric,
  historyData,
  historyLines,
  localBucketTime,
} from "./historyChart";
import { setLanguage } from "./i18n";
import type { HistoryPoint } from "./api";

afterEach(() => {
  setLanguage("en");
  vi.restoreAllMocks();
});

describe("history chart semantics", () => {
  it.each([60, 3600])(
    "keeps counter last and delta separate at %i seconds",
    (resolution_seconds) => {
      for (const mode of ["value", "delta"]) {
        const lines = historyLines(
          { kind: "counter", resolution_seconds },
          mode,
        );
        expect(lines).toHaveLength(1);
        expect(lines[0]!.label).toContain(
          mode === "delta" ? "(delta)" : "(last)",
        );
        const points = [
          { time: 0, value: 123, delta: null },
          { time: 60, value: 123, delta: 0 },
        ] as HistoryPoint[];
        expect(historyData(points, lines)).toEqual([
          [0, 60],
          mode === "delta" ? [null, 0] : [123, 123],
        ]);
      }
    },
  );
  it.each(["gauge", "rate"] as const)(
    "labels %s observations and hourly aggregates accurately",
    (kind) => {
      const minute = historyLines({ kind, resolution_seconds: 60 }, "value");
      const hourly = historyLines({ kind, resolution_seconds: 3600 }, "value");
      expect(minute[0]!.label).toBe(
        kind === "rate"
          ? "Five-second observation (value)"
          : "Observation (value)",
      );
      expect(hourly[0]!.label).toBe(
        kind === "rate"
          ? "Observed weighted average (avg)"
          : "Observed average (avg)",
      );
      const points = [
        { time: 0, value: 10.125, min: 0, max: 13 },
        { time: 3600, value: null, min: null, max: null },
      ] as HistoryPoint[];
      expect(historyData(points, hourly)).toEqual([
        [0, 3600],
        [10.125, null],
        [0, null],
        [13, null],
      ]);
    },
  );
  it("retains exact API precision and base units, distinguishing missing values from zero", () => {
    expect(exactMetric(1048577, "bytes")).toBe("1048577 B");
    expect(exactMetric(0.1234567890123456, "bytes_per_second")).toBe(
      "0.1234567890123456 B/s",
    );
    expect(exactMetric(3600.123456789, "seconds")).toBe("3600.123456789 s");
    expect(exactMetric(123456789, "count")).toBe("123456789");
    expect(exactMetric(0, "bytes")).toBe("0 B");
    for (const value of [null, undefined, NaN, Infinity, -1])
      expect(exactMetric(value, "bytes")).toBe("—");
  });
  it("formats bucket timestamps in the browser timezone with an offset, including epoch zero", () => {
    const original = Intl.DateTimeFormat;
    const formats: Intl.DateTimeFormatOptions[] = [];
    vi.spyOn(Intl, "DateTimeFormat").mockImplementation(
      function (locales, options) {
        formats.push(options || {});
        return new original(locales, options);
      },
    );
    for (const locale of ["en", "zh-CN"] as const) {
      setLanguage(locale);
      for (const seconds of [0, 1793511000, 1793514600]) {
        expect(localBucketTime(seconds)).toBe(
          new original(locale, {
            year: "numeric",
            month: "2-digit",
            day: "2-digit",
            hour: "2-digit",
            minute: "2-digit",
            second: "2-digit",
            timeZoneName: "shortOffset",
          }).format(new Date(seconds * 1000)),
        );
      }
    }
    expect(formats.every((options) => options.timeZone === undefined)).toBe(
      true,
    );
  });
});
