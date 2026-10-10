import { fireEvent, screen, waitFor, within } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import AppCachePage from "./AppCachePage.vue";
import { MAINTENANCE_POLL_MS } from "@/features/http-cache";
import type { Schema } from "@/shared/api";
import { renderAppPage } from "@/test/appPage";
import {
  codexApp,
  appConfiguration,
  autoCleanupStatus,
  cacheEntry,
  httpCacheApp,
  httpCacheConfiguration,
  maintenanceItem,
  maintenanceItemPage,
  maintenancePreview,
  prewarmOptions,
  retentionStatus,
  sourceEpoch,
} from "@/test/factories/runtime";
import { apiError, mockApi, useHandlers } from "@/test/msw";

type Preview = Schema<"MaintenancePreview">;

const mirror = { key: "example/mirror", tab: "cache" };

/** The card (section) titled `title`. */
function card(title: string): HTMLElement {
  const section = screen.getByRole("heading", { name: title }).closest("section");
  if (!section) throw new Error(`no card ${title}`);
  return section;
}

function httpCacheHandlers(app = httpCacheApp()) {
  return [
    mockApi("get", "/admin/api/apps/{vendor}/{app}", () => app),
    mockApi("get", "/admin/api/apps/{vendor}/{app}/sources", () => ({
      items: [
        sourceEpoch({
          epoch: 1,
          current: false,
          base_url: undefined,
          base_urls: ["https://old.example.com"],
        }),
        sourceEpoch({
          base_url: undefined,
          base_urls: ["https://origin.example.com"],
          source_strategy: "ordered",
        }),
      ],
    })),
    mockApi("get", "/admin/api/apps/{vendor}/{app}/cache/entries", ({ request }) => {
      const epoch = new URL(request.url).searchParams.get("source_epoch");
      return {
        items: [
          cacheEntry(epoch === "1" ? { path: "/old/file.bin", generation_id: "gen-old" } : {}),
        ],
        next_cursor: null,
      };
    }),
    mockApi("get", "/admin/api/cache/auto-cleanup", () => autoCleanupStatus()),
    mockApi("get", "/admin/api/apps/{vendor}/{app}/prewarm/options", () =>
      prewarmOptions({ kind: "http_cache", channels: [], platforms: [] }),
    ),
    mockApi("get", "/admin/api/apps/{vendor}/{app}/configuration", () => httpCacheConfiguration()),
  ];
}

/** A preview that builds for one poll, then is ready (or, once executed, runs and finishes). */
function maintenanceServer(kind: "cleanup" | "refresh") {
  const state: { preview: Preview; reads: number; created?: unknown; executed: boolean } = {
    preview: maintenancePreview({
      kind,
      state: "building",
      ...(kind === "refresh" ? { basis: null, before: null } : {}),
    }),
    reads: 0,
    executed: false,
  };
  const base = `/admin/api/apps/{vendor}/{app}/cache/${kind}` as const;
  const advance = () => {
    state.reads++;
    if (state.preview.state === "building") state.preview = { ...state.preview, state: "ready" };
    else if (state.preview.state === "running") {
      state.preview = {
        ...state.preview,
        state: "done",
        completed_files: 2,
        result: {
          kind: "refresh",
          selected_files: 2,
          completed_files: 2,
          refreshed: 1,
          not_modified: 1,
          stale_fallback: 0,
          failed: 0,
          skipped: 0,
        },
      };
    }
    return state.preview;
  };
  const items = () =>
    maintenanceItemPage([
      maintenanceItem({
        result_status: state.executed ? (kind === "cleanup" ? "retired" : "refreshed") : "pending",
      }),
      maintenanceItem({ ordinal: 1, generation_id: "gen-b", path: "/releases/old.zip" }),
    ]);
  const handlers =
    kind === "cleanup"
      ? [
          mockApi("post", `${base}/preview`, async ({ request }) => {
            state.created = await request.json();
            return state.preview;
          }),
          mockApi("get", `${base}/{preview_id}`, advance),
          mockApi("get", `${base}/{preview_id}/items`, items),
        ]
      : [
          mockApi(
            "post",
            "/admin/api/apps/{vendor}/{app}/cache/refresh/preview",
            async ({ request }) => {
              state.created = await request.json();
              return state.preview;
            },
          ),
          mockApi("get", "/admin/api/apps/{vendor}/{app}/cache/refresh/{preview_id}", advance),
          mockApi("get", "/admin/api/apps/{vendor}/{app}/cache/refresh/{preview_id}/items", items),
        ];
  return { state, handlers };
}

describe("cache tab of an HTTP cache application", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("lists cached files per source and refreshes a single file", async () => {
    let refreshed: unknown;
    useHandlers(
      ...httpCacheHandlers(),
      mockApi(
        "post",
        "/admin/api/apps/{vendor}/{app}/cache/refresh",
        async ({ request }) => {
          refreshed = await request.json();
          return {
            path: "/releases/1.2.3/tool.zip",
            generation_id: "gen-z",
            status: "stale_fallback" as const,
            reason: "upstream: timeout",
          };
        },
        { status: 200 },
      ),
    );
    await renderAppPage(AppCachePage, mirror);
    const user = userEvent.setup();

    const table = await screen.findByRole("table", { name: "Cached files" });
    expect(await within(table).findByText("/releases/1.2.3/tool.zip")).toBeInTheDocument();
    expect(within(table).getByText("Fresh")).toBeInTheDocument();
    expect(await screen.findByText(/3 applications · 120 files scanned/)).toBeInTheDocument();

    await user.click(
      within(table).getByRole("button", { name: "Refresh /releases/1.2.3/tool.zip" }),
    );
    expect(
      await screen.findByText(
        "The origin failed; the cached copy of /releases/1.2.3/tool.zip is kept.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByText("upstream: timeout")).toBeInTheDocument();
    expect(refreshed).toEqual({ path: "/releases/1.2.3/tool.zip" });

    // Historical sources can be browsed and cleaned, but not refreshed.
    await user.click(screen.getByRole("combobox", { name: "Cache source" }));
    await user.click(await screen.findByRole("option", { name: /Source 1 \(historical\)/ }));
    expect(await within(table).findByText("/old/file.bin")).toBeInTheDocument();
    expect(within(table).queryByRole("button", { name: /Refresh/ })).toBeNull();
    await user.click(screen.getByRole("tab", { name: "Refresh and cleanup" }));
    expect(await screen.findByText(/Refreshing works only on the current source/)).toBeVisible();
  });

  it("previews a cleanup with a UTC cutoff, waits for the frozen list and executes it", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const server = maintenanceServer("cleanup");
    useHandlers(
      ...httpCacheHandlers(),
      ...server.handlers,
      mockApi(
        "post",
        "/admin/api/apps/{vendor}/{app}/cache/cleanup/{preview_id}/execute",
        () => {
          server.state.executed = true;
          server.state.preview = {
            ...server.state.preview,
            state: "done",
            result: {
              kind: "cleanup",
              selected_files: 2,
              retired_files: 1,
              skipped_accessed: 1,
              skipped_changed: 0,
              retired_bytes: 2048,
            },
          };
          return server.state.preview;
        },
        { status: 200 },
      ),
    );
    await renderAppPage(AppCachePage, mirror);
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime.bind(vi) });
    await user.click(await screen.findByRole("tab", { name: "Refresh and cleanup" }));

    const cleanup = card("Clean up files");
    const pattern = within(cleanup).getByRole("textbox", { name: /Path pattern/ });
    await user.clear(pattern);
    await user.type(pattern, "/releases/");
    await user.click(within(cleanup).getByRole("radio", { name: /Last download/ }));
    const cutoff = within(cleanup).getByLabelText(/Older than \(local time/);
    await fireEvent.update(cutoff, "2026-09-01T12:30");
    const utc = new Date(2026, 8, 1, 12, 30).toISOString();
    expect(within(cleanup).getByText(`UTC cutoff: ${utc}`)).toBeInTheDocument();
    await user.click(within(cleanup).getByRole("button", { name: "Preview cleanup" }));

    const review = await within(cleanup).findByRole("region", { name: "Cleanup preview" });
    expect(server.state.created).toEqual({
      match: { type: "glob", pattern: "/releases/" },
      basis: "fetched_at",
      before: utc,
    });
    expect(within(review).getByText("Building")).toBeInTheDocument();
    expect(within(review).queryByRole("table")).toBeNull();

    await vi.advanceTimersByTimeAsync(MAINTENANCE_POLL_MS);
    expect(await within(review).findByText("Ready")).toBeInTheDocument();
    expect(await within(review).findByText("/releases/old.zip")).toBeInTheDocument();

    await user.click(within(review).getByRole("button", { name: "Remove selected files" }));
    await user.click(
      within(await screen.findByRole("alertdialog")).getByRole("button", {
        name: "Remove selected files",
      }),
    );
    expect(await within(review).findByText("Cleanup finished")).toBeInTheDocument();
    expect(within(review).getByText(/Removed 1 of 2 files/)).toBeInTheDocument();
    expect(await within(review).findByText("Removed")).toBeInTheDocument();
  });

  it("discards an expired cleanup preview instead of executing it", async () => {
    const server = maintenanceServer("cleanup");
    server.state.preview = { ...server.state.preview, state: "ready" };
    useHandlers(
      ...httpCacheHandlers(),
      ...server.handlers,
      mockApi("post", "/admin/api/apps/{vendor}/{app}/cache/cleanup/{preview_id}/execute", () =>
        apiError("PREVIEW_NOT_FOUND"),
      ),
    );
    await renderAppPage(AppCachePage, mirror);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("tab", { name: "Refresh and cleanup" }));
    const cleanup = card("Clean up files");
    await fireEvent.update(within(cleanup).getByLabelText(/Older than/), "2026-09-01T00:00");
    await user.click(within(cleanup).getByRole("button", { name: "Preview cleanup" }));
    const review = await within(cleanup).findByRole("region", { name: "Cleanup preview" });
    await user.click(await within(review).findByRole("button", { name: "Remove selected files" }));
    await user.click(
      within(await screen.findByRole("alertdialog")).getByRole("button", {
        name: "Remove selected files",
      }),
    );
    expect(await within(cleanup).findByText("Preview no longer valid")).toBeInTheDocument();
    expect(within(cleanup).queryByRole("region", { name: "Cleanup preview" })).toBeNull();
  });

  it("refreshes matching files in the background and shows the summary", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const server = maintenanceServer("refresh");
    useHandlers(
      ...httpCacheHandlers(),
      ...server.handlers,
      mockApi(
        "post",
        "/admin/api/apps/{vendor}/{app}/cache/refresh/{preview_id}/execute",
        () => {
          server.state.executed = true;
          server.state.preview = { ...server.state.preview, state: "running" };
          return server.state.preview;
        },
        { status: 202 },
      ),
    );
    await renderAppPage(AppCachePage, mirror);
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime.bind(vi) });
    await user.click(await screen.findByRole("tab", { name: "Refresh and cleanup" }));
    const panel = card("Refresh files");
    await user.click(within(panel).getByRole("button", { name: "Preview refresh" }));
    const review = await within(panel).findByRole("region", { name: "Refresh preview" });
    expect(server.state.created).toEqual({ match: { type: "glob", pattern: "/" } });
    await vi.advanceTimersByTimeAsync(MAINTENANCE_POLL_MS);
    await user.click(await within(review).findByRole("button", { name: "Refresh selected files" }));
    await user.click(
      within(await screen.findByRole("alertdialog")).getByRole("button", {
        name: "Refresh selected files",
      }),
    );
    expect(await within(review).findByText("Running")).toBeInTheDocument();
    // The form stays locked while the job runs.
    expect(within(panel).getByRole("button", { name: "Preview refresh" })).toBeDisabled();

    await vi.advanceTimersByTimeAsync(MAINTENANCE_POLL_MS);
    expect(await within(review).findByText("Refresh finished")).toBeInTheDocument();
    expect(within(review).getByText(/1 refreshed · 1 not modified/)).toBeInTheDocument();
    await waitFor(() => {
      expect(within(review).getAllByText("Refreshed").length).toBeGreaterThan(0);
    });
    const reads = server.state.reads;
    await vi.advanceTimersByTimeAsync(MAINTENANCE_POLL_MS * 3);
    expect(server.state.reads).toBe(reads);
  });
});

describe("cache tab of a release application", () => {
  it("shows prewarm, retention and version cleanup", async () => {
    useHandlers(
      mockApi("get", "/admin/api/apps/{vendor}/{app}", () => codexApp()),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/sources", () => ({ items: [sourceEpoch()] })),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/prewarm/options", () => prewarmOptions()),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/configuration", () => appConfiguration()),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/retention/status", () => retentionStatus()),
    );
    await renderAppPage(AppCachePage);
    expect(await screen.findByRole("heading", { name: "Prewarm" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Keep latest versions" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Version cleanup" })).toBeInTheDocument();
    expect(screen.queryByRole("tab")).toBeNull();
  });

  it("keeps a deleted application read-only", async () => {
    useHandlers(
      mockApi("get", "/admin/api/apps/{vendor}/{app}", () =>
        codexApp({ deleted_at: "2026-10-09T00:00:00Z", enabled: false }),
      ),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/sources", () => ({ items: [sourceEpoch()] })),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/prewarm/options", () => prewarmOptions()),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/configuration", () => appConfiguration()),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/retention/status", () => retentionStatus()),
    );
    await renderAppPage(AppCachePage);
    await screen.findByRole("heading", { name: "Prewarm" });
    expect(screen.queryByRole("button", { name: "Start prewarming" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Save retention" })).toBeNull();
    expect(screen.getByRole("textbox", { name: /Minimum version to keep/ })).toBeDisabled();
  });
});
