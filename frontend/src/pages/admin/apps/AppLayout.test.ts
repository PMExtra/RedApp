import { screen, waitFor, within } from "@testing-library/vue";
import { describe, expect, it } from "vitest";
import type { Schema } from "@/shared/api";
import { renderAdminPage } from "@/test/directory";
import { app, appConfiguration, notes, page, vendor } from "@/test/factories/directory";
import { appStatus } from "@/test/factories/metrics";
import { apiError, mockApi, useHandlers } from "@/test/msw";

function appServer(
  overrides: Partial<Schema<"App">>,
  vendorOverrides: Partial<Schema<"Vendor">> = {},
) {
  useHandlers(
    mockApi("get", "/admin/api/vendors/{vendor}", () => vendor(vendorOverrides)),
    mockApi("get", "/admin/api/apps/{vendor}/{app}", () => app(overrides)),
    mockApi("get", "/admin/api/apps/{vendor}/{app}/admin-notes", () => notes()),
    mockApi("get", "/admin/api/apps/{vendor}/{app}/configuration", () => appConfiguration()),
  );
}

const emptyCursorPage = { items: [], next_cursor: null };

async function tabNames() {
  const nav = await screen.findByRole("navigation", { name: "Sections" });
  return within(nav)
    .getAllByRole("link")
    .map((link) => link.textContent.trim());
}

describe("application tabs", () => {
  it.each([
    ["info", ["Settings", "Admin notes"]],
    ["hosted", ["Settings", "Files", "Admin notes"]],
    ["http-cache", ["Settings", "Cache", "Admin notes"]],
    ["codex", ["Settings", "Versions", "Cache", "Admin notes"]],
    ["claude-code", ["Settings", "Versions", "Cache", "Admin notes"]],
  ] as const)("offers the %s tabs", async (provider, tabs) => {
    appServer({ provider });
    await renderAdminPage("/admin/vendors/example/apps/tools/admin-notes");
    expect(await tabNames()).toEqual(tabs);
  });

  it.each([
    [{ provider: "info" }, "settings"],
    [{ provider: "codex" }, "versions"],
    [{ provider: "codex", deleted_at: "2026-10-01T08:00:00Z" }, "settings"],
  ] as const)("opens the default tab of %o", async (overrides, tab) => {
    appServer(overrides);
    useHandlers(
      mockApi("get", "/admin/api/categories", () => page([], { limit: 100 })),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/status", () => appStatus()),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/versions", () => emptyCursorPage),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/resources", () => emptyCursorPage),
    );
    const { router } = await renderAdminPage("/admin/vendors/example/apps/tools");
    await waitFor(() => {
      expect(router.currentRoute.value.path).toBe(`/admin/vendors/example/apps/tools/${tab}`);
    });
  });

  it("shows a missing tab as unavailable instead of the page", async () => {
    appServer({ provider: "info" });
    await renderAdminPage("/admin/vendors/example/apps/tools/cache");
    expect(await screen.findByText("Not available for this application")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Open settings" })).toHaveAttribute(
      "href",
      "/admin/vendors/example/apps/tools/settings",
    );
  });

  it("hides the versions of a deleted application and makes it read-only", async () => {
    appServer({ provider: "codex", deleted_at: "2026-10-01T08:00:00Z" });
    await renderAdminPage("/admin/vendors/example/apps/tools/admin-notes");
    expect(await tabNames()).toEqual(["Settings", "Cache", "Admin notes"]);
    expect(screen.getByText(/This application is deleted/)).toBeInTheDocument();
    expect(await screen.findByRole("textbox", { name: "Notes" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Export configuration" })).toBeNull();
  });

  it("links published applications to the public page and copies the download URL", async () => {
    appServer({ provider: "http-cache" });
    await renderAdminPage("/admin/vendors/example/apps/tools/admin-notes");
    expect(await screen.findByRole("link", { name: "Public page" })).toHaveAttribute(
      "href",
      "/example/tools",
    );
    expect(screen.getByRole("button", { name: "Copy download URL" })).toBeInTheDocument();
  });

  it("offers no public link while the vendor is disabled", async () => {
    appServer({ provider: "http-cache" }, { enabled: false });
    await renderAdminPage("/admin/vendors/example/apps/tools/admin-notes");
    expect(await screen.findByText("Disabled by vendor")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Public page" })).toBeNull();
  });

  it("explains a missing application", async () => {
    useHandlers(
      mockApi("get", "/admin/api/vendors/{vendor}", () => vendor()),
      mockApi("get", "/admin/api/apps/{vendor}/{app}", () => apiError("APPLICATION_NOT_FOUND")),
    );
    await renderAdminPage("/admin/vendors/example/apps/gone/settings");
    expect(await screen.findByText("Application not found")).toBeInTheDocument();
  });
});
