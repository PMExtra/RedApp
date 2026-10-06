import { expect, it } from "vitest";
import { adminReturnPath } from "./router";
it("keeps login return targets inside administration after URL normalization", () => {
  for (const target of ["https://evil.example", "//evil.example", "/admin/../../evil", "/admin/%2e%2e/evil", "/admin/../admin/login", "/admin/%6cogin", "/admin/\\evil", "/admin/%5cevil", "/admin/%0aevil", "/admin/%ZZ", ["/admin/overview"], undefined]) {
    expect(adminReturnPath(target)).toBe("/admin/vendors");
  }
  for (const target of ["/admin/vendors/openai/apps/codex/versions", "/admin/vendors/anthropic/apps/claude-code/settings", "/admin/events?path=%2Ffiles#details"]) expect(adminReturnPath(target)).toBe(target);
});
