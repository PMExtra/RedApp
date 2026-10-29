import { describe, expect, it } from "vitest";
import { appTabs, defaultAppTab } from "./links";

describe("application tabs", () => {
  it("follows the provider", () => {
    const tabs = (provider: Parameters<typeof appTabs>[0]["provider"]) =>
      appTabs({ provider, deleted_at: null });
    expect(tabs("codex")).toEqual([
      "settings",
      "versions",
      "cache",
      "metrics",
      "events",
      "admin-notes",
    ]);
    expect(tabs("json-release")).toEqual([
      "settings",
      "versions",
      "cache",
      "metrics",
      "events",
      "admin-notes",
    ]);
    expect(tabs("http-cache")).toEqual(["settings", "cache", "metrics", "events", "admin-notes"]);
    expect(tabs("hosted")).toEqual(["settings", "files", "events", "admin-notes"]);
    expect(defaultAppTab({ provider: "json-release", deleted_at: null })).toBe("versions");
    expect(appTabs({ provider: "json-release", deleted_at: "2026-10-01T00:00:00Z" })).toEqual([
      "settings",
      "cache",
      "metrics",
      "events",
      "admin-notes",
    ]);
  });
});
