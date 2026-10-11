import { screen, waitFor, within } from "@testing-library/vue";
import { describe, expect, it } from "vitest";
import { renderAdminPage } from "@/test/directory";
import { app, notes, vendor } from "@/test/factories/directory";
import { resource, version } from "@/test/factories/runtime";
import { mockApi, useHandlers } from "@/test/msw";

const none = { complete: 0, in_progress: 0, interrupted: 0, failed: 0, retired: 0 };

function releaseServer(requested: string[]) {
  useHandlers(
    mockApi("get", "/admin/api/vendors/{vendor}", () => vendor()),
    mockApi("get", "/admin/api/apps/{vendor}/{app}", () => app({ provider: "codex" })),
    mockApi("get", "/admin/api/apps/{vendor}/{app}/admin-notes", () => notes()),
    mockApi("get", "/admin/api/apps/{vendor}/{app}/versions", ({ request }) => {
      const cursor = new URL(request.url).searchParams.get("cursor");
      return cursor === "page-2"
        ? { items: [version({ version: "0.40.0", files: none })], next_cursor: null }
        : {
            items: [
              version(),
              version({ version: "0.45.0", files: { ...none, failed: 1, retired: 2 } }),
            ],
            next_cursor: "page-2",
          };
    }),
    mockApi("get", "/admin/api/apps/{vendor}/{app}/resources", ({ request }) => {
      const filter = new URL(request.url).searchParams.get("version") ?? "";
      requested.push(filter);
      return {
        items: [
          resource(),
          resource({ id: "gen-0002", version: "0.45.0", state: "failed", error: "upstream: 502" }),
        ].filter((item) => item.version === filter),
        next_cursor: null,
      };
    }),
  );
}

describe("versions tab", () => {
  it("summarizes the stored files of each version and pages through versions", async () => {
    releaseServer([]);
    const { user } = await renderAdminPage("/admin/vendors/example/apps/tools/versions");

    const versions = await screen.findByRole("table", { name: "Versions" });
    const latest = await within(versions).findByRole("row", { name: /0\.46\.0/ });
    expect(within(latest).getByText("1 complete")).toBeInTheDocument();
    const older = within(versions).getByRole("row", { name: /0\.45\.0/ });
    expect(within(older).getByText("1 failed")).toBeInTheDocument();
    expect(within(older).getByText("2 awaiting deletion")).toBeInTheDocument();
    expect(screen.queryByRole("table", { name: /Stored files/ })).toBeNull();

    const pager = screen.getByRole("navigation", { name: "Pagination" });
    await user.click(within(pager).getByRole("button", { name: /Next page/ }));
    expect(await within(versions).findByText("0.40.0")).toBeInTheDocument();
    expect(within(versions).getByText("Not cached")).toBeInTheDocument();
    expect(within(pager).getByText("Page 2")).toBeInTheDocument();
    await user.click(within(pager).getByRole("button", { name: /Previous page/ }));
    expect(await within(versions).findByText("0.46.0")).toBeInTheDocument();
  });

  it("opens the stored files of one version on its own page and goes back", async () => {
    const requested: string[] = [];
    releaseServer(requested);
    const { router, user } = await renderAdminPage("/admin/vendors/example/apps/tools/versions");

    await user.click(await screen.findByRole("link", { name: "Show files of 0.45.0" }));
    await waitFor(() => {
      expect(router.currentRoute.value.path).toBe(
        "/admin/vendors/example/apps/tools/versions/0.45.0",
      );
    });
    const files = await screen.findByRole("table", { name: "Stored files of 0.45.0" });
    expect(await within(files).findByText("Failed")).toBeInTheDocument();
    expect(within(files).getByText("upstream: 502")).toBeInTheDocument();
    expect(requested.every((filter) => filter === "0.45.0")).toBe(true);

    // The versions tab stays selected and the breadcrumbs lead back to it.
    const tabs = screen.getByRole("navigation", { name: "Sections" });
    expect(within(tabs).getByRole("link", { name: "Versions" })).toHaveClass("!text-fg");
    const crumbs = screen.getByRole("navigation", { name: "Breadcrumbs" });
    expect(within(crumbs).getByText("0.45.0")).toBeInTheDocument();

    await user.click(screen.getByRole("link", { name: "All versions" }));
    expect(await screen.findByRole("table", { name: "Versions" })).toBeInTheDocument();
    expect(router.currentRoute.value.path).toBe("/admin/vendors/example/apps/tools/versions");
  });

  it("opens a version page directly", async () => {
    releaseServer([]);
    await renderAdminPage("/admin/vendors/example/apps/tools/versions/0.46.0");
    const files = await screen.findByRole("table", { name: "Stored files of 0.46.0" });
    expect(await within(files).findByText("linux-x64/codex")).toBeInTheDocument();
  });
});
