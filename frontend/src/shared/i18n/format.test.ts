import { describe, expect, it } from "vitest";
import { resolveLocale } from "./index";
import { formatBytes, formatDuration, formatRelativeTime, pickLocalized } from "./format";

describe("resolveLocale", () => {
  it.each([
    ["en-US", "en"],
    ["EN", "en"],
    ["zh", "zh-CN"],
    ["zh-TW", "zh-CN"],
    ["zh-Hant-HK", "zh-CN"],
    ["fr-FR", undefined],
    ["", undefined],
    [null, undefined],
  ])("%s → %s", (tag, expected) => {
    expect(resolveLocale(tag)).toBe(expected);
  });
});

describe("formatting", () => {
  it("formats bytes in IEC units with two decimals", () => {
    expect(formatBytes("en", 0)).toBe("0.00 B");
    expect(formatBytes("en", 1023)).toBe("1,023.00 B");
    expect(formatBytes("en", 1024)).toBe("1.00 KiB");
    expect(formatBytes("en", 40860.52 * 1024 ** 2)).toBe("39.90 GiB");
    expect(formatBytes("en", 1024 ** 4)).toBe("1.00 TiB");
    for (const unknown of [undefined, null, -1, Number.NaN, Number.POSITIVE_INFINITY]) {
      expect(formatBytes("en", unknown)).toBe("—");
    }
  });

  it("formats durations in the largest exact unit", () => {
    expect(formatDuration("en", 90)).toBe("90 sec");
    expect(formatDuration("en", 7200)).toBe("2 hr");
    expect(formatDuration("en", 86400 * 30)).toBe("30 days");
    expect(formatDuration("zh-CN", 300)).toBe("5分钟");
  });

  it("formats relative times", () => {
    const now = new Date("2026-10-10T12:00:00Z");
    expect(formatRelativeTime("en", "2026-10-10T11:57:00Z", now)).toBe("3 minutes ago");
    expect(formatRelativeTime("en", "2026-10-10T14:00:00Z", now)).toBe("in 2 hours");
    expect(formatRelativeTime("en", "not a date", now)).toBe("—");
  });

  it("falls back to the other language for empty localized texts", () => {
    expect(pickLocalized({ en: "Name", "zh-CN": "" }, "zh-CN")).toBe("Name");
    expect(pickLocalized({ en: "", "zh-CN": "名称" }, "en")).toBe("名称");
    expect(pickLocalized({ en: "Name", "zh-CN": "名称" }, "zh-CN")).toBe("名称");
  });
});
