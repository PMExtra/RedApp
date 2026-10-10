import { screen, waitFor, within } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import AppVersionsPage from "./AppVersionsPage.vue";
import { renderAppPage } from "@/test/appPage";
import { resource, version } from "@/test/factories/runtime";
import { mockApi, useHandlers } from "@/test/msw";

function releaseHandlers(counter: { resources: string[] }) {
  return [
    mockApi("get", "/admin/api/apps/{vendor}/{app}/versions", ({ request }) => {
      const cursor = new URL(request.url).searchParams.get("cursor");
      return cursor === "page-2"
        ? { items: [version({ version: "0.40.0" })], next_cursor: null }
        : { items: [version(), version({ version: "0.45.0" })], next_cursor: "page-2" };
    }),
    mockApi("get", "/admin/api/apps/{vendor}/{app}/resources", ({ request }) => {
      const filter = new URL(request.url).searchParams.get("version") ?? "";
      counter.resources.push(filter);
      return {
        items: [
          resource({ version: filter || "0.46.0" }),
          resource({ id: "gen-0002", version: "0.45.0", state: "failed", error: "upstream: 502" }),
        ].filter((item) => !filter || item.version === filter),
        next_cursor: null,
      };
    }),
  ];
}

describe("versions tab", () => {
  it("shows versions and stored files, and filters files by version", async () => {
    const counter = { resources: [] as string[] };
    useHandlers(...releaseHandlers(counter));
    await renderAppPage(AppVersionsPage, { tab: "versions" });
    const user = userEvent.setup();

    const versions = await screen.findByRole("table", { name: "Versions" });
    expect(await within(versions).findByText("0.46.0")).toBeInTheDocument();
    const files = await screen.findByRole("table", { name: "Stored files" });
    expect(within(files).getByText("Failed")).toBeInTheDocument();
    expect(within(files).getByText("upstream: 502")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Show files of 0.45.0" }));
    await waitFor(() => {
      expect(counter.resources).toContain("0.45.0");
    });
    expect(screen.getByRole("button", { name: "Show files of 0.45.0" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(screen.getByText("Version 0.45.0")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Show all versions" }));
    expect(screen.queryByText("Version 0.45.0")).toBeNull();

    // Cursor pagination: next page, then back.
    const pager = screen.getAllByRole("navigation", { name: "Pagination" })[0];
    if (!pager) throw new Error("no pagination");
    await user.click(within(pager).getByRole("button", { name: /Next page/ }));
    expect(await within(versions).findByText("0.40.0")).toBeInTheDocument();
    expect(within(pager).getByText("Page 2")).toBeInTheDocument();
    await user.click(within(pager).getByRole("button", { name: /Previous page/ }));
    expect(await within(versions).findByText("0.46.0")).toBeInTheDocument();
  });
});
