import { flushPromises } from "@vue/test-utils";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { Metric } from "../api";
import { boot, mountPage, resetStores, response, status } from "../testSupport";

beforeEach(resetStores);
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  document.body.innerHTML = "";
});

it("refreshes collapsed diagnostics and routes version history through the active SPA scope", async () => {
  vi.useFakeTimers();
  const metric: Metric = {
    key: "versions.total",
    label: "Discovered versions",
    kind: "gauge",
    unit: "count",
    group: "Resources and tasks",
    value: 2,
    observed_seconds: 0,
  };
  let value = 2;
  const fetch = vi.fn(async (url: string) => {
    if (url === "/api/bootstrap") return response(boot);
    if (url.endsWith("/session")) return response({ csrf: "token" });
    if (url.includes("/history?"))
      return response({
        ...metric,
        range: "7d",
        resolution_seconds: 3600,
        from: 0,
        to: 3600,
        points: [],
      });
    if (url.endsWith("/status"))
      return response({ ...status, metrics: [{ ...metric, value }] });
    return response({ items: [], next_cursor: null });
  });
  vi.stubGlobal("fetch", fetch);
  const { wrapper, router } = await mountPage("/admin/overview");
  const diagnostics = wrapper.get("details.diagnostic-metrics")
    .element as HTMLDetailsElement;
  expect(diagnostics.open).toBe(false);
  value = 3;
  await vi.advanceTimersByTimeAsync(5000);
  await flushPromises();
  expect(diagnostics.open).toBe(false);
  expect(wrapper.get('[data-metric="versions.total"] strong').text()).toBe("3");
  diagnostics.open = true;
  await wrapper.get('[data-metric="versions.total"]').trigger("click");
  await flushPromises();
  expect(
    fetch.mock.calls.some(
      ([url]) =>
        url === "/admin/api/history?metric=versions.total&range=7d&scope=global",
    ),
  ).toBe(true);
  expect(wrapper.get(".version-scope-note").text()).toContain(
    "Includes all applications",
  );

  await router.push("/admin/apps/anthropic/claude-code/versions");
  await flushPromises();
  expect(wrapper.find(".history-dialog").exists()).toBe(false);
  expect(wrapper.get('[data-metric="versions.total"]').text()).toContain(
    "Includes only this application",
  );
  (
    wrapper.get("details.diagnostic-metrics").element as HTMLDetailsElement
  ).open = true;
  await wrapper.get('[data-metric="versions.total"]').trigger("click");
  await flushPromises();
  expect(
    fetch.mock.calls.some(
      ([url]) =>
        url ===
        "/admin/api/apps/anthropic/claude-code/history?metric=versions.total&range=7d",
    ),
  ).toBe(true);
  expect(wrapper.get(".version-scope-note").text()).toContain(
    "Includes only this application",
  );
  await router.push("/admin/apps/openai/codex/versions");
  await flushPromises();
  expect(wrapper.find(".history-dialog").exists()).toBe(false);
  wrapper.unmount();
});
