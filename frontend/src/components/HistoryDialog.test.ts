import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, describe, it, expect, vi } from "vitest";
import type { HistorySeries, Metric } from "../api";
const charts = vi.hoisted(() => ({
  create: vi.fn(),
  destroy: vi.fn(),
  resize: vi.fn(),
}));
vi.mock("uplot", () => ({
  default: class {
    static tzDate(date: Date) {
      return date;
    }
    constructor(...args: unknown[]) {
      charts.create(...args);
    }
    destroy() {
      charts.destroy();
    }
    setSize(size: unknown) {
      charts.resize(size);
    }
  },
}));
import HistoryDialog from "./HistoryDialog.vue";
const metric: Metric = {
  key: "disk.cache_bytes",
  label: "Cache",
  kind: "gauge",
  unit: "bytes",
  group: "Disk",
  value: 1024,
  observed_seconds: 0,
};
const series: HistorySeries = {
  ...metric,
  range: "7d",
  resolution_seconds: 3600,
  from: 0,
  to: 3600,
  points: [
    {
      time: 0,
      value: 10,
      min: 8,
      max: 12,
      avg: 10,
      last: 11,
      count: 60,
      delta: null,
      delta_count: 0,
      observed_seconds: 0,
      partial: false,
      incomplete: false,
    },
    {
      time: 3600,
      value: null,
      min: null,
      max: null,
      avg: null,
      last: null,
      count: 0,
      delta: null,
      delta_count: 0,
      observed_seconds: 0,
      partial: true,
      incomplete: true,
    },
  ],
};
const response = (data: unknown, status = 200) => ({
  ok: status === 200,
  status,
  json: async () => data,
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});
describe("History dialog", () => {
  it("defaults to 7d, switches all windows, preserves gaps and closes", async () => {
    const fetch = vi.fn(async (_url: string, _options?: RequestInit) =>
      response(series),
    );
    vi.stubGlobal("fetch", fetch);
    const wrapper = mount(HistoryDialog, {
      props: { metric },
      attachTo: document.body,
    });
    await flushPromises();
    expect(fetch.mock.calls.length).toBe(1);
    expect(wrapper.find("[aria-pressed=true]").text()).toBe("7 days");
    expect(charts.create.mock.calls[0][0].series[1].spanGaps).toBe(false);
    expect(charts.create.mock.calls[0][1][1]).toEqual([10, null]);
    for (const label of ["24 hours", "30 days", "7 days"]) {
      await wrapper
        .findAll("button")
        .find((button) => button.text() === label)!
        .trigger("click");
      await flushPromises();
    }
    const ranges = fetch.mock.calls
      .map((call) => (call[1] as RequestInit).headers as Record<string, string>)
      .map((headers) => headers["X-History-Range"]);
    expect(ranges).toEqual(["7d", "24h", "30d", "7d"]);
    expect(wrapper.text()).toContain("Current hour is partial");
    await wrapper.find("summary").trigger("click");
    expect(wrapper.text()).toContain("1970-01-01T00:00:00.000Z");
    await wrapper
      .findAll("button")
      .find((button) => button.text() === "Close history")!
      .trigger("click");
    expect(wrapper.emitted("close")).toHaveLength(1);
    wrapper.unmount();
    expect(charts.destroy).toHaveBeenCalled();
  });
  it("shows error/retry and empty history without a chart", async () => {
    let fail = true;
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        fail
          ? response({ error: "Read failed" }, 503)
          : response({ ...series, points: [] }),
      ),
    );
    const wrapper = mount(HistoryDialog, {
      props: { metric },
      attachTo: document.body,
    });
    await flushPromises();
    expect(wrapper.find("[role=alert]").text()).toContain("Read failed");
    expect(wrapper.emitted("error")).toBeUndefined();
    fail = false;
    await wrapper
      .findAll("button")
      .find((button) => button.text() === "Retry history")!
      .trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("No observations available");
    expect(wrapper.find("[role=alert]").exists()).toBe(false);
    expect(charts.create).not.toHaveBeenCalled();
    wrapper.unmount();
  });
  it("cancels replaced/unmounted requests and reports session expiration", async () => {
    let cancelled = 0;
    const fetch = vi.fn(
      (_url: string, options?: RequestInit) =>
        new Promise((_resolve, reject) => {
          options!.signal!.addEventListener("abort", () => {
            cancelled++;
            reject(new DOMException("Cancelled", "AbortError"));
          });
        }),
    );
    vi.stubGlobal("fetch", fetch);
    const wrapper = mount(HistoryDialog, {
      props: { metric },
      attachTo: document.body,
    });
    await wrapper
      .findAll("button")
      .find((button) => button.text() === "24 hours")!
      .trigger("click");
    expect(cancelled).toBe(1);
    wrapper.unmount();
    expect(cancelled).toBe(2);
    await flushPromises();
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => response({ error: "Sign in required" }, 401)),
    );
    const expired = mount(HistoryDialog, {
      props: { metric },
      attachTo: document.body,
    });
    await flushPromises();
    expect(expired.emitted("error")).toHaveLength(1);
    expired.unmount();
  });
  it("shows unknown counter increments as gaps rather than zero", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        response({
          ...series,
          kind: "counter",
          points: [
            {
              ...series.points[0],
              value: 100,
              last: 100,
              min: null,
              max: null,
              avg: null,
              delta: null,
            },
          ],
        }),
      ),
    );
    const wrapper = mount(HistoryDialog, {
      props: { metric: { ...metric, kind: "counter" } },
      attachTo: document.body,
    });
    await flushPromises();
    expect(wrapper.text()).toContain("Cumulative counters are never averaged");
    await wrapper.find("select").setValue("delta");
    await flushPromises();
    expect(wrapper.text()).toContain("No observations available");
    wrapper.unmount();
  });
});
