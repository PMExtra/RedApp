import { describe, expect, it } from "vitest";
import { adminMessages } from "@/app/admin/messages";
import { publicMessages } from "@/app/public/messages";
import { errorCatalog } from "@/shared/api";
import { flattenMessages, mergeLocaleModules, type LocaleModule } from "@/shared/i18n/messages";

const english = import.meta.glob<LocaleModule>("/src/**/locales/**/en.ts", {
  eager: true,
  import: "default",
});
const chinese = import.meta.glob<LocaleModule>("/src/**/locales/**/zh-CN.ts", {
  eager: true,
  import: "default",
});

function placeholders(message: string): string[] {
  return [...message.matchAll(/\{(\w+)\}/g)].map((match) => match[1] ?? "").sort();
}

describe("locale files", () => {
  it("come in en / zh-CN pairs", () => {
    const pairs = Object.keys(english).map((file) => file.replace(/en\.ts$/, "zh-CN.ts"));
    expect(Object.keys(chinese).sort()).toEqual(pairs.sort());
  });

  it.each(Object.keys(english))("%s has the same keys and placeholders in zh-CN", (file) => {
    const en = flattenMessages(english[file] ?? {});
    const zh = flattenMessages(chinese[file.replace(/en\.ts$/, "zh-CN.ts")] ?? {});
    expect([...zh.keys()].sort()).toEqual([...en.keys()].sort());
    for (const [key, text] of en) {
      expect(placeholders(zh.get(key) ?? ""), key).toEqual(placeholders(text));
      expect(zh.get(key)?.trim(), key).not.toBe("");
    }
  });

  it("never define a namespace twice within an entry", () => {
    for (const entry of [publicMessages, adminMessages]) {
      expect(() => mergeLocaleModules(entry.en)).not.toThrow();
      expect(() => mergeLocaleModules(entry["zh-CN"])).not.toThrow();
    }
  });

  it("keep admin-only texts out of the public entry", () => {
    const files = Object.keys(publicMessages.en);
    expect(
      files.some((file) => file.includes("/app/admin/") || file.includes("/pages/admin/")),
    ).toBe(false);
    expect(files.every((file) => !/\/features\/[^/]+\/locales\/en\.ts$/.test(file))).toBe(true);
  });

  it("translate every error code of the spec catalog", () => {
    const en = flattenMessages(mergeLocaleModules(adminMessages.en));
    const missing = Object.keys(errorCatalog).filter((code) => !en.has(`errors.codes.${code}`));
    expect(missing).toEqual([]);
  });
});
