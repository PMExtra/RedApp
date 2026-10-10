import { describe, expect, it } from "vitest";
import { safeReturnPath } from "./returnTo";

describe("safeReturnPath", () => {
  it.each([
    ["/admin/vendors?q=codex#top", "/admin/vendors?q=codex#top"],
    ["/admin/vendors/openai/apps/codex/settings", "/admin/vendors/openai/apps/codex/settings"],
    [undefined, "/admin/overview"],
    [["/admin/vendors"], "/admin/overview"],
    ["https://evil.example/admin/x", "/admin/overview"],
    ["//evil.example/admin/x", "/admin/overview"],
    ["/admin/login?returnTo=/admin/x", "/admin/overview"],
    ["/admin/api/session", "/admin/overview"],
    ["/admin/\\evil", "/admin/overview"],
    ["/admin/a%20b", "/admin/overview"],
    ["/admin/x\r\nSet-Cookie: a", "/admin/overview"],
    ["/all", "/admin/overview"],
  ])("%j → %s", (value, expected) => {
    expect(safeReturnPath(value)).toBe(expected);
  });
});
