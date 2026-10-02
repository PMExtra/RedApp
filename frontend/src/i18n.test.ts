import { readFileSync } from "node:fs";
import { afterEach, it, expect, vi } from "vitest";
import { localDate, messages, label, setLanguage } from "./i18n";
afterEach(() => {
  setLanguage("en");
  vi.restoreAllMocks();
});
it("covers all 43 metric labels and preserves interpolation keys in Chinese", () => {
  const source = readFileSync("../internal/history/catalog.go", "utf8");
  const names = [
    ...source.matchAll(/\{"[a-z_]+", "([^"]+)"/g),
    ...source.matchAll(/add\("[^"]+", "([^"]+)"/g),
  ].map((match) => match[1]);
  expect(names).toHaveLength(43);
  setLanguage("zh-CN");
  for (const name of names) {
    expect(messages).toHaveProperty(name);
    expect(label(name)).not.toBe(name);
  }
  for (const [en, zh] of Object.entries(messages))
    expect(zh.match(/\{\w+\}/g)?.sort() || []).toEqual(
      en.match(/\{\w+\}/g)?.sort() || [],
    );
});
it("formats snapshot using browser timezone and rejects absent timestamps", () => {
  const original = Intl.DateTimeFormat;
  const formats: Intl.DateTimeFormatOptions[] = [];
  vi.spyOn(Intl, "DateTimeFormat").mockImplementation(
    function (locales, options) {
      formats.push(options || {});
      return new original(locales, options);
    },
  );
  const value = "2026-10-02T08:01:00Z";
  expect(localDate(value)).toBe(
    new original("en", {
      year: "numeric",
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      timeZoneName: "short",
    }).format(new Date(value)),
  );
  expect(formats[0].timeZone).toBeUndefined();
  expect(localDate("bad")).toBe("—");
  expect(localDate("0001-01-01T00:00:00Z")).toBe("—");
});
