import { screen, waitFor, within } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import AppVersionsPage from "./AppVersionsPage.vue";
import { renderAppPage } from "@/test/appPage";
import {
  adminApp,
  appStatus,
  historySeries,
  httpCacheApp,
  resource,
  version,
} from "@/test/factories/runtime";
import { mockApi, useHandlers } from "@/test/msw";

function releaseHandlers(counter: { status: number; resources: string[] }) {
  return [
    mockApi("get", "/admin/api/apps/{vendor}/{app}", () => adminApp()),
    mockApi("get", "/admin/api/apps/{vendor}/{app}/status", () => {
      counter.status++;
      return appStatus();
    }),
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
  afterEach(() => {
    vi.useRealTimers();
  });

  it("shows metrics, versions and stored files, and filters files by version", async () => {
    const counter = { status: 0, resources: [] as string[] };
    useHandlers(...releaseHandlers(counter));
    await renderAppPage(AppVersionsPage, { tab: "versions" });
    const user = userEvent.setup();

    expect(await screen.findByRole("button", { name: /Versions: 3/ })).toBeInTheDocument();
    expect(screen.getByText("5.00 MiB")).toBeInTheDocument();
    const versions = await screen.findByRole("table", { name: "Versions" });
    expect(within(versions).getByText("0.46.0")).toBeInTheDocument();
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

  it("refreshes metrics every 5 seconds until auto refresh is turned off", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const counter = { status: 0, resources: [] as string[] };
    useHandlers(...releaseHandlers(counter));
    await renderAppPage(AppVersionsPage, { tab: "versions" });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime.bind(vi) });
    await screen.findByRole("button", { name: /Versions: 3/ });
    const before = counter.status;

    await vi.advanceTimersByTimeAsync(5_000);
    await waitFor(() => {
      expect(counter.status).toBe(before + 1);
    });

    await user.click(screen.getByRole("switch", { name: "Auto refresh" }));
    const stopped = counter.status;
    await vi.advanceTimersByTimeAsync(15_000);
    expect(counter.status).toBe(stopped);
  });

  it("opens the history of a metric and loads another range", async () => {
    const ranges: string[] = [];
    useHandlers(
      ...releaseHandlers({ status: 0, resources: [] }),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/history", ({ request }) => {
        const range = new URL(request.url).searchParams.get("range") ?? "";
        ranges.push(range);
        return historySeries({ range: range as "24h" });
      }),
    );
    await renderAppPage(AppVersionsPage, { tab: "versions" });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /Versions: 3/ }));

    const dialog = await screen.findByRole("dialog", { name: "Versions" });
    expect(await within(dialog).findByRole("table", { name: "History of Versions" })).toBeVisible();
    await user.click(within(dialog).getByRole("radio", { name: "7 days" }));
    await waitFor(() => {
      expect(ranges).toEqual(["24h", "7d"]);
    });
  });

  it("explains that other providers have no versions", async () => {
    useHandlers(mockApi("get", "/admin/api/apps/{vendor}/{app}", () => httpCacheApp()));
    await renderAppPage(AppVersionsPage, { key: "example/mirror", tab: "versions" });
    expect(await screen.findByText("Not available for this application")).toBeInTheDocument();
  });
});
