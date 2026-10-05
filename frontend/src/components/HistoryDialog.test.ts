import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, describe, it, expect, vi } from "vitest";
import type { HistorySeries, Metric } from "../api";
import type uPlot from "uplot";
import { localBucketTime } from "../historyChart";
import { setLanguage } from "../i18n";
const charts = vi.hoisted(() => ({
  create: vi.fn(),
  destroy: vi.fn(),
  resize: vi.fn(),
  cursor: vi.fn(),
  instances: [] as {
    options: uPlot.Options;
    cursor: uPlot.Cursor;
    over: HTMLDivElement;
  }[],
}));
vi.mock("uplot", () => ({
  default: class {
    cursor: uPlot.Cursor = { idx: null, left: -10, top: -10 };
    over = document.createElement("div");
    static tzDate(date: Date) {
      return date;
    }
    constructor(
      public options: uPlot.Options,
      public data: uPlot.AlignedData,
      host: HTMLElement,
    ) {
      charts.create(options, data, host);
      this.over.getBoundingClientRect = () => new DOMRect(100, 20, 200, 200);
      host.append(this.over);
      charts.instances.push(this);
    }
    destroy() {
      charts.destroy();
      this.over.remove();
    }
    setSize(size: unknown) {
      charts.resize(size);
    }
    valToPos(value: number) {
      return this.data[0].indexOf(value) * 100;
    }
    posToIdx(left: number) {
      return Math.round(left / 100);
    }
    setCursor(position: { left: number; top: number }, fireHook = true) {
      charts.cursor(position, fireHook);
      this.cursor = {
        ...position,
        idx: position.left < 0 ? null : this.posToIdx(position.left),
      };
      if (fireHook)
        this.options.hooks?.setCursor?.forEach((hook) =>
          hook?.(this as unknown as uPlot),
        );
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
  setLanguage("en");
  charts.instances.length = 0;
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});
async function hover(index: number | null) {
  const chart = charts.instances.at(-1)!;
  chart.cursor = {
    idx: index,
    left: index === null ? -10 : index * 100,
    top: index === null ? -10 : 20,
  };
  chart.options.hooks!.setCursor!.forEach((hook) =>
    hook?.(chart as unknown as uPlot),
  );
  await flushPromises();
}
describe("History dialog", () => {
  it.each([undefined, "anthropic/claude-code"])(
    "describes fresh version history for scope %s without old migration assumptions",
    async (application) => {
      const versionMetric: Metric = {
        ...metric,
        key: "versions.total",
        label: "Discovered versions",
        unit: "count",
      };
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => response({ ...series, ...versionMetric })),
      );
      const wrapper = mount(HistoryDialog, {
        props: { metric: versionMetric, application },
        attachTo: document.body,
      });
      await flushPromises();
      expect(wrapper.get(".version-scope-note").text()).toContain(
        application
          ? "Includes only this application"
          : "Includes all applications",
      );
      expect(wrapper.text()).not.toContain("Codex only");
      expect(wrapper.text()).not.toContain("transition");
      await hover(0);
      expect(wrapper.get(".history-readout dd").text()).toBe("10");
      setLanguage("zh-CN");
      await flushPromises();
      expect(wrapper.get(".version-scope-note").text()).toContain(
        application ? "仅包含当前应用" : "包含所有应用",
      );
      wrapper.unmount();
    },
  );
  it("aborts replaced application requests, discards late results, and clears stale points on failure", async () => {
    const pending: {
      url: string;
      signal: AbortSignal;
      resolve: (data: unknown) => void;
    }[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(
        (url: string, init: RequestInit) =>
          new Promise((resolve) => {
            pending.push({ url, signal: init.signal!, resolve });
          }),
      ),
    );
    const wrapper = mount(HistoryDialog, {
      props: { metric, application: "openai/codex" },
      attachTo: document.body,
    });
    await wrapper.setProps({ application: "anthropic/claude-code" });
    expect(pending[0]!.signal.aborted).toBe(true);
    expect(pending[1]!.url).toContain("/apps/anthropic/claude-code/history?");
    expect(pending[1]!.url).not.toContain("scope=");
    pending[1]!.resolve(response(series));
    await flushPromises();
    await hover(0);
    expect(wrapper.get(".history-readout dd").text()).toBe("10 B");
    pending[0]!.resolve(
      response({ ...series, points: [{ ...series.points[0], value: 999 }] }),
    );
    await flushPromises();
    expect(wrapper.get(".history-readout dd").text()).toBe("10 B");
    await wrapper.setProps({ application: undefined });
    expect(pending[2]!.url).toContain("&scope=global");
    expect(wrapper.find(".history-readout").exists()).toBe(false);
    pending[2]!.resolve(response({}, 503));
    await flushPromises();
    expect(wrapper.get('[role="alert"]').text()).toContain(
      "Service temporarily unavailable",
    );
    expect(wrapper.find(".history-chart").exists()).toBe(false);
    wrapper.unmount();
  });
  it("reads all hovered series with exact units and local bucket time, including gaps", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        response({
          ...series,
          points: [
            { ...series.points[0], value: 1048577.125, min: 0, max: 2097153 },
            series.points[1],
          ],
        }),
      ),
    );
    const wrapper = mount(HistoryDialog, {
      props: { metric },
      attachTo: document.body,
    });
    await flushPromises();
    await hover(0);
    const readout = () => wrapper.get(".history-readout");
    expect(readout().text()).toContain("Observed average (avg)");
    expect(readout().text()).toContain("1048577.125 B");
    expect(readout().text()).toContain("Observed minimum (min)");
    expect(readout().text()).toContain("0 B");
    expect(readout().text()).toContain("2097153 B");
    expect(readout().get("time").text()).toBe(localBucketTime(0));
    expect(readout().get("time").attributes("datetime")).toBe(
      "1970-01-01T00:00:00.000Z",
    );
    expect(readout().attributes("aria-live")).toBe("off");
    const chart = charts.instances.at(-1)!;
    expect(
      chart.options.cursor!.dataIdx!(chart as unknown as uPlot, 1, 1, 3600),
    ).toBe(1);
    await hover(1);
    expect(
      readout()
        .findAll("dd")
        .map((item) => item.text()),
    ).toEqual(["—", "—", "—"]);
    expect(readout().text()).toContain("No observation in this bucket");
    expect(readout().text()).toContain("Partial current bucket");
    await hover(null);
    expect(readout().find("time").exists()).toBe(false);
    wrapper.unmount();
  });
  it("supports keyboard traversal through gaps, Escape, and touch without snapping across missing buckets", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => response(series)),
    );
    const wrapper = mount(HistoryDialog, {
      props: { metric },
      attachTo: document.body,
    });
    await flushPromises();
    const host = wrapper.get(".history-chart");
    expect(host.attributes("tabindex")).toBe("0");
    (host.element as HTMLElement).focus();
    await flushPromises();
    expect(wrapper.get(".history-readout").text()).toContain("No observation");
    await host.trigger("keydown", { key: "ArrowLeft" });
    expect(wrapper.get(".history-readout time").text()).toBe(
      localBucketTime(0),
    );
    expect(wrapper.get(".history-readout").attributes("aria-live")).toBe(
      "polite",
    );
    await host.trigger("keydown", { key: "Home" });
    await host.trigger("keydown", { key: "ArrowLeft" });
    expect(wrapper.get(".history-readout time").text()).toBe(
      localBucketTime(0),
    );
    await host.trigger("keydown", { key: "End" });
    await host.trigger("keydown", { key: "ArrowRight" });
    expect(wrapper.get(".history-readout time").text()).toBe(
      localBucketTime(3600),
    );
    await host.trigger("keydown", { key: "Escape" });
    expect(wrapper.find(".history-readout time").exists()).toBe(false);
    expect(wrapper.emitted("close")).toBeUndefined();
    expect(charts.cursor).toHaveBeenLastCalledWith(
      { left: -10, top: -10 },
      false,
    );
    await host.trigger("pointerdown", {
      pointerType: "touch",
      clientX: 100,
      clientY: 40,
    });
    expect(wrapper.get(".history-readout time").text()).toBe(
      localBucketTime(0),
    );
    await host.trigger("pointerdown", {
      pointerType: "touch",
      clientX: 200,
      clientY: 40,
    });
    expect(wrapper.get(".history-readout").text()).toContain("No observation");
    await host.trigger("pointerdown", {
      pointerType: "touch",
      clientX: 20,
      clientY: 40,
    });
    expect(wrapper.get(".history-readout time").text()).toBe(
      localBucketTime(3600),
    );
    // A tap can emit compatibility mouse events; keep the touched bucket.
    await hover(0);
    await hover(null);
    expect(wrapper.get(".history-readout time").text()).toBe(
      localBucketTime(3600),
    );
    await host.trigger("pointermove", { pointerType: "mouse" });
    await hover(0);
    expect(wrapper.get(".history-readout time").text()).toBe(
      localBucketTime(0),
    );
    await host.trigger("blur");
    expect(wrapper.find(".history-readout time").exists()).toBe(false);
    wrapper.unmount();
  });
  it.each(["24h", "7d", "30d"])(
    "uses actual %s rate resolution, coverage and clears stale selection on changes",
    async (range) => {
      const data = {
        ...series,
        kind: "rate",
        unit: "bytes_per_second",
        range,
        resolution_seconds: range === "24h" ? 60 : 3600,
        points: [
          {
            ...series.points[0],
            value: 0.123456789,
            min: range === "24h" ? 0.123456789 : 0.1,
            max: range === "24h" ? 0.123456789 : 1.25,
            count: range === "24h" ? 1 : 2,
            observed_seconds: range === "24h" ? 5 : 10,
            partial: true,
            incomplete: true,
          },
        ],
      };
      vi.stubGlobal(
        "fetch",
        vi.fn(async () => response(data)),
      );
      const wrapper = mount(HistoryDialog, {
        props: {
          metric: { ...metric, kind: "rate", unit: "bytes_per_second" },
        },
        attachTo: document.body,
      });
      await flushPromises();
      if (range !== "7d") {
        await wrapper
          .findAll("button")
          .find(
            (button) =>
              button.text() === (range === "24h" ? "24 hours" : "30 days"),
          )!
          .trigger("click");
        await flushPromises();
      }
      await hover(0);
      expect(wrapper.get(".history-readout").text()).toContain(
        range === "24h"
          ? "Five-second observation (value)"
          : "Observed weighted average (avg)",
      );
      expect(wrapper.get(".history-readout").text()).toContain(
        "0.123456789 B/s",
      );
      expect(wrapper.get(".history-readout").text()).toContain(
        `Observed duration: ${range === "24h" ? 5 : 10} seconds.`,
      );
      setLanguage("zh-CN");
      await flushPromises();
      expect(wrapper.find(".history-readout time").exists()).toBe(false);
      await hover(0);
      expect(wrapper.get(".history-readout").text()).toContain(
        "浏览器本地时间",
      );
      await wrapper
        .findAll("button")
        .find((button) => button.text() === "7 天")!
        .trigger("click");
      await flushPromises();
      expect(wrapper.find(".history-readout time").exists()).toBe(false);
      wrapper.unmount();
    },
  );
  it("shows counter last or observed delta, distinguishing unknown increments from observed zero", async () => {
    const data = {
      ...series,
      kind: "counter",
      points: [
        { ...series.points[0], value: 1001, last: 1001, delta: null },
        {
          ...series.points[0],
          time: 3600,
          value: 1001,
          last: 1001,
          delta: 0,
          delta_count: 1,
          observed_seconds: 60,
        },
      ],
    };
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => response(data)),
    );
    const wrapper = mount(HistoryDialog, {
      props: { metric: { ...metric, kind: "counter", unit: "count" } },
      attachTo: document.body,
    });
    await flushPromises();
    await hover(0);
    expect(wrapper.get(".history-readout").text()).toContain(
      "Last cumulative value (last)",
    );
    expect(wrapper.get(".history-readout dd").text()).toBe("1001");
    const chooser = wrapper.get('[role="combobox"]');
    await chooser.trigger("keydown", { key: "ArrowDown" });
    await chooser.trigger("keydown", { key: "End" });
    await chooser.trigger("keydown", { key: "Enter" });
    await flushPromises();
    expect(wrapper.find(".history-readout time").exists()).toBe(false);
    await hover(0);
    expect(wrapper.get(".history-readout").text()).toContain(
      "Observed increment (delta)",
    );
    expect(wrapper.get(".history-readout").text()).toContain(
      "Increment unknown",
    );
    expect(wrapper.get(".history-readout dd").text()).toBe("—");
    await hover(1);
    expect(wrapper.get(".history-readout dd").text()).toBe("0");
    expect(wrapper.get(".history-readout").text()).toContain(
      "Valid intervals: 1",
    );
    expect(wrapper.get(".history-readout").text()).not.toContain(
      "Increment unknown",
    );
    wrapper.unmount();
  });
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
    expect(String(vi.mocked(fetch).mock.calls[0]![0])).toContain(
      "&scope=global",
    );
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
    const ranges = fetch.mock.calls.map((call) =>
      new URL(String(call[0]), "https://test.example").searchParams.get(
        "range",
      ),
    );
    expect(ranges).toEqual(["7d", "24h", "30d", "7d"]);
    expect(wrapper.text()).toContain("Current bucket is partial");
    await wrapper.find("summary").trigger("click");
    expect(wrapper.text()).toContain("01/01/1970");
    await wrapper.find('[aria-label="Close history"]').trigger("click");
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
      props: { metric, application: "anthropic/claude-code" },
      attachTo: document.body,
    });
    await flushPromises();
    expect(String(vi.mocked(fetch).mock.calls[0]![0])).toContain(
      "/admin/api/apps/anthropic/claude-code/history?",
    );
    expect(String(vi.mocked(fetch).mock.calls[0]![0])).not.toContain("scope=");
    expect(wrapper.find("[role=alert]").text()).toContain(
      "Service temporarily unavailable",
    );
    expect(wrapper.emitted("error")).toBeUndefined();
    fail = false;
    await wrapper
      .findAll("button")
      .find((button) => button.attributes("aria-label") === "Retry history")!
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
    expect(wrapper.find("select").exists()).toBe(false);
    const chooser = wrapper.get('[role="combobox"]');
    await chooser.trigger("keydown", { key: "ArrowDown" });
    await chooser.trigger("keydown", { key: "End" });
    await chooser.trigger("keydown", { key: "Escape" });
    expect(wrapper.emitted("close")).toBeUndefined();
    expect(wrapper.text()).not.toContain("No observations available");
    await chooser.trigger("keydown", { key: "ArrowDown" });
    await chooser.trigger("keydown", { key: "End" });
    await chooser.trigger("keydown", { key: "Enter" });
    await flushPromises();
    expect(wrapper.text()).toContain("No observations available");
    wrapper.unmount();
  });
});
