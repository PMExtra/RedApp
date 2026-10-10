import { screen, waitFor, within } from "@testing-library/vue";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import AppMetricsPage from "./AppMetricsPage.vue";
import { renderAppPage } from "@/test/appPage";
import { appStatus, historySeries } from "@/test/factories/metrics";
import { mockApi, useHandlers } from "@/test/msw";

vi.mock("uplot", () => import("@/test/uplot"));

function statusHandler(counter: { status: number }) {
  return mockApi("get", "/admin/api/apps/{vendor}/{app}/status", () => {
    counter.status++;
    return appStatus({ "versions.total": 3, "counters.downstream_bytes": 5 * 1024 * 1024 });
  });
}

describe("metrics tab", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("shows the application metrics with explanations behind info icons", async () => {
    useHandlers(statusHandler({ status: 0 }));
    await renderAppPage(AppMetricsPage, { tab: "metrics" });

    expect(await screen.findByRole("button", { name: /^Versions: 3/ })).toBeInTheDocument();
    expect(screen.getByText("5.00 MiB")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "About Traffic out" })).toBeInTheDocument();
    // Explanations are not printed under the values.
    expect(screen.queryByText("File bytes sent to clients.", { exact: false })).toBeNull();
  });

  it("refreshes metrics every 5 seconds until auto refresh is turned off", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const counter = { status: 0 };
    useHandlers(statusHandler(counter));
    await renderAppPage(AppMetricsPage, { tab: "metrics" });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime.bind(vi) });
    await screen.findByRole("button", { name: /^Versions: 3/ });
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
      statusHandler({ status: 0 }),
      mockApi("get", "/admin/api/apps/{vendor}/{app}/history", ({ request }) => {
        const url = new URL(request.url);
        const range = url.searchParams.get("range") as "24h" | "7d" | "30d";
        ranges.push(`${url.pathname} ${url.searchParams.get("metric") ?? ""} ${range}`);
        return historySeries("versions.total", [2, 3], {
          range,
          scope: "app",
          app_key: "openai/codex",
        });
      }),
    );
    await renderAppPage(AppMetricsPage, { tab: "metrics" });
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /^Versions: 3/ }));

    const dialog = await screen.findByRole("dialog", { name: "Versions" });
    expect(await within(dialog).findByText(/^Latest 3, lowest 2, highest 3\./)).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "24 hours" }));
    await waitFor(() => {
      expect(ranges).toEqual([
        "/admin/api/apps/openai/codex/history versions.total 7d",
        "/admin/api/apps/openai/codex/history versions.total 24h",
      ]);
    });
  });
});
