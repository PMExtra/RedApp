import { fireEvent, screen, waitFor, within } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RouterView } from "vue-router";
import { globalStatus, historySeries } from "@/test/factories/metrics";
import { apiError, mockApi, useHandlers } from "@/test/msw";
import { renderWithApp } from "@/test/render";
import OverviewPage from "./OverviewPage.vue";

// happy-dom has no canvas: replace uPlot by a stand-in that keeps the
// geometry the chart component asks for (10 px per bucket).
vi.mock("uplot", () => ({
  default: class {
    cursor = { idx: null as number | null, left: -10, top: -10 };
    over = document.createElement("div");
    constructor(
      public options: unknown,
      public data: number[][],
      host: HTMLElement,
    ) {
      this.over.getBoundingClientRect = () => new DOMRect(0, 0, 1000, 200);
      host.append(this.over);
    }
    destroy() {
      this.over.remove();
    }
    setSize() {}
    valToPos(value: number) {
      return (this.data[0]?.indexOf(value) ?? 0) * 10;
    }
    posToIdx(left: number) {
      return Math.round(left / 10);
    }
    setCursor(position: { left: number; top: number }) {
      this.cursor = { ...position, idx: position.left < 0 ? null : this.posToIdx(position.left) };
    }
  },
}));

function renderOverview() {
  return renderWithApp(RouterView, {
    routes: [{ path: "/admin/overview", component: OverviewPage }],
    path: "/admin/overview",
  });
}

afterEach(() => {
  vi.useRealTimers();
});

describe("overview", () => {
  it("groups common and diagnostic metrics and refreshes every 5 seconds", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let calls = 0;
    useHandlers(
      mockApi("get", "/admin/api/status", () => {
        calls++;
        return globalStatus({ "disk.free_bytes": 1024 * calls });
      }),
    );
    await renderOverview();

    const common = (await screen.findByRole("heading", { name: "Common metrics" })).closest(
      "section",
    ) as HTMLElement;
    expect(within(common).getByRole("heading", { name: "Disk" })).toBeInTheDocument();
    expect(within(common).getByRole("button", { name: /^Free space: 1\.00 KiB/ })).toBeVisible();
    // Diagnostic metrics start collapsed.
    const goroutines = screen.getByRole("button", { name: /^Goroutines:/, hidden: true });
    expect(goroutines.closest("details")).not.toHaveAttribute("open");

    await vi.advanceTimersByTimeAsync(5_000);
    expect(
      await within(common).findByRole("button", { name: /^Free space: 2\.00 KiB/ }),
    ).toBeVisible();

    await userEvent
      .setup({ advanceTimers: (ms) => vi.advanceTimersByTime(ms) })
      .click(screen.getByRole("switch", { name: "Auto refresh" }));
    const before = calls;
    await vi.advanceTimersByTimeAsync(15_000);
    expect(calls).toBe(before);
  });

  it("keeps the last good snapshot when a refresh fails", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    let failing = false;
    useHandlers(
      mockApi("get", "/admin/api/status", () =>
        failing ? apiError("STORAGE_UNAVAILABLE") : globalStatus({ "disk.free_bytes": 2048 }),
      ),
    );
    await renderOverview();
    expect(await screen.findByRole("button", { name: /^Free space: 2\.00 KiB/ })).toBeVisible();

    failing = true;
    await vi.advanceTimersByTimeAsync(5_000);
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Refresh failed. Showing the last successful snapshot.");
    expect(alert).toHaveTextContent("Server storage is unavailable.");
    expect(screen.getByRole("button", { name: /^Free space: 2\.00 KiB/ })).toBeVisible();

    failing = false;
    await vi.advanceTimersByTimeAsync(5_000);
    await waitFor(() => {
      expect(screen.queryByRole("alert")).toBeNull();
    });
  });

  it("shows a metric's history with range switching, gaps and a keyboard readout", async () => {
    const ranges: string[] = [];
    useHandlers(
      mockApi("get", "/admin/api/status", () => globalStatus({ "disk.free_bytes": 4096 })),
      mockApi("get", "/admin/api/history", ({ request }) => {
        const url = new URL(request.url);
        const range = url.searchParams.get("range") as "24h" | "7d" | "30d";
        ranges.push(`${url.searchParams.get("metric") ?? ""} ${range}`);
        return range === "24h"
          ? historySeries("disk.free_bytes", [1024, null, 3072], {
              range,
              resolution_seconds: 60,
            })
          : historySeries("disk.free_bytes", [1024, 2048, null, 4096], { range });
      }),
    );
    await renderOverview();
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /^Free space:/ }));

    const dialog = await screen.findByRole("dialog", { name: "Free space" });
    expect(
      await within(dialog).findByText(
        "Latest 4.00 KiB, lowest 1.00 KiB, highest 4.00 KiB. 3 of 4 time buckets have data.",
      ),
    ).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "7 days" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );

    await user.click(within(dialog).getByRole("button", { name: "24 hours" }));
    expect(
      await within(dialog).findByText(
        "Latest 3.00 KiB, lowest 1.00 KiB, highest 3.00 KiB. 2 of 3 time buckets have data.",
      ),
    ).toBeInTheDocument();
    expect(ranges).toEqual(["disk.free_bytes 7d", "disk.free_bytes 24h"]);

    // Keyboard: focus selects the newest sample; arrows step into the gap.
    const chart = within(dialog).getByRole("group", { name: "Chart of Free space over 24 hours" });
    chart.focus();
    const readout = within(dialog).getByRole("status", { name: "Selected time bucket" });
    await waitFor(() => {
      expect(readout).toHaveTextContent("3.00 KiB");
    });
    await user.keyboard("{ArrowLeft}");
    expect(readout).toHaveTextContent("No sample in this time bucket.");
    await user.keyboard("{Home}");
    expect(readout).toHaveTextContent("1.00 KiB");

    // Touch: a tap pins the readout to the bucket under the finger.
    await fireEvent.pointerDown(chart, { pointerType: "touch", clientX: 20, clientY: 10 });
    expect(readout).toHaveTextContent("3.00 KiB");

    // Table fallback lists the buckets that have data.
    await user.click(within(dialog).getByText("Data table (2 buckets with data)"));
    const table = await within(dialog).findByRole("table");
    expect(within(table).getAllByRole("row")).toHaveLength(3);
  });
});
