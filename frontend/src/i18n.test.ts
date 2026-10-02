import { readFileSync } from "node:fs";
import { afterEach, it, expect, vi } from "vitest";
import { localDate, messages, label, setLanguage } from "./i18n";
afterEach(() => {
  setLanguage("en");
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
it("covers active and retired metric labels and preserves interpolation keys in Chinese", () => {
  const source = readFileSync("../internal/history/catalog.go", "utf8");
  const names = [
    ...source.matchAll(/\{"[a-z_]+", "([^"]+)"/g),
    ...source.matchAll(/add\("[^"]+", "([^"]+)"/g),
    ...source.matchAll(/Definition\{Key: "[^"]+", Label: "([^"]+)"/g),
  ].map((match) => match[1]);
  expect(names.length).toBeGreaterThan(0);
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

it("resolves manual and ordered browser preferences without persisting automatic guesses", async () => {
  const { initialLanguage } = await import("./i18n");
  const get = vi.fn();
  const set = vi.fn();
  vi.stubGlobal("localStorage", { getItem: get, setItem: set });
  get.mockReturnValue(null);
  for (const [languages, single, expected] of [
    [["fr", "zh-Hant-TW", "en"], "en", "zh-CN"],
    [["de", "en-US", "zh-CN"], "zh-CN", "en"],
    [["fr"], "zh-CN", "en"],
    [[], "zh-HK", "zh-CN"],
    [["zh;q=1", "zh_CN", "*"], "zh-CN", "en"],
  ] as const) {
    Object.defineProperty(navigator, "languages", {
      value: languages,
      configurable: true,
    });
    Object.defineProperty(navigator, "language", {
      value: single,
      configurable: true,
    });
    expect(initialLanguage()).toBe(expected);
  }
  get.mockReturnValue("zh-CN");
  expect(initialLanguage()).toBe("zh-CN");
  get.mockImplementation(() => {
    throw Error("denied");
  });
  Object.defineProperty(navigator, "languages", {
    get() {
      throw Error("denied");
    },
    configurable: true,
  });
  expect(initialLanguage()).toBe("zh-CN");
  expect(set).not.toHaveBeenCalled();
});
