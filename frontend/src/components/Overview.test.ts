import { mount } from "@vue/test-utils";
import { afterEach, expect, it } from "vitest";
import type { Metric, Status } from "../api";
import { setLanguage } from "../i18n";
import Overview from "./Overview.vue";
import Events from "./Events.vue";

// Full pre-upgrade inventory also verifies stale snapshots cannot restore the
// two retired cards. The label catalog is checked independently by i18n.test.
const inventory = {
  disk: [
    "used_bytes",
    "logical_bytes",
    "allocated_cache_bytes",
    "allocated_temporary_bytes",
    "allocated_pending_bytes",
    "cache_bytes",
    "temporary_bytes",
    "pending_bytes",
    "other_bytes",
    "free_bytes",
  ],
  counters: [
    "requests",
    "artifact_requests",
    "cache_hit_requests",
    "shared_follower_requests",
    "miss_requests",
    "reuse_requests",
    "download_success",
    "download_errors",
    "upstream_errors",
    "upstream_bytes",
    "downstream_bytes",
    "cleanup_freed_bytes",
  ],
  rates: ["upstream_bytes_per_second", "downstream_bytes_per_second"],
  runtime: ["memory_bytes", "goroutines", "uptime_seconds"],
  resources: [
    "total",
    "current",
    "retired",
    "readers",
    "active_writers",
    "queued",
    "downloading",
    "resuming",
    "retry_wait",
    "verifying",
    "complete",
    "failed",
    "invalid",
    "interrupted",
  ],
  versions: ["total"],
  events: ["recent_total"],
};
const metrics: Metric[] = Object.entries(inventory).flatMap(
  ([group, names]) =>
    names.map((name) => ({
      key: `${group}.${name}`,
      label: `${group}.${name}`,
      group,
      kind:
        group === "counters"
          ? "counter"
          : group === "rates"
            ? "rate"
            : "gauge",
      unit: name === "uptime_seconds" ? "seconds" : "count",
      value: name === "uptime_seconds" ? 90000 : 7,
      observed_seconds: 0,
    })),
);
afterEach(() => setLanguage("en"));

it("keeps 16 common and 25 collapsed diagnostics, opens their history and preserves failure details", async () => {
  const wrapper = mount(Overview, {
    props: { status: { metrics } as Status },
  });
  expect(wrapper.findAll(".common-metrics .metric-card")).toHaveLength(16);
  expect(wrapper.findAll(".diagnostic-metrics .metric-card")).toHaveLength(
    25,
  );
  const diagnostics = wrapper.get("details.diagnostic-metrics");
  expect((diagnostics.element as HTMLDetailsElement).open).toBe(false);
  expect(
    diagnostics.get("summary .disclosure-icon svg").attributes("viewBox"),
  ).toBe("0 0 24 24");
  expect(
    wrapper.get('[data-metric="runtime.uptime_seconds"] strong').text(),
  ).toBe("1.04 d");
  expect(
    wrapper.find('[data-metric="counters.reuse_requests"]').exists(),
  ).toBe(false);
  expect(wrapper.find('[data-metric="events.recent_total"]').exists()).toBe(
    false,
  );
  await wrapper.get('[data-metric="disk.free_bytes"]').trigger("click");
  // Native details supplies keyboard/touch toggling; expanding leaves all
  // diagnostic history buttons usable, including integrity failures.
  (diagnostics.element as HTMLDetailsElement).open = true;
  await wrapper.get('[data-metric="resources.invalid"]').trigger("click");
  expect(wrapper.emitted("history")?.map(([m]) => (m as Metric).key)).toEqual(
    ["disk.free_bytes", "resources.invalid"],
  );
  expect(wrapper.get('[data-metric="versions.total"]').text()).toContain(
    "Includes all applications",
  );
  await wrapper.setProps({ application: "anthropic/claude-code" });
  expect(wrapper.get('[data-metric="versions.total"]').text()).toContain(
    "Includes only this application",
  );
  expect(wrapper.text()).not.toContain("Includes all applications");
  setLanguage("zh-CN");
  await wrapper.vm.$nextTick();
  expect(wrapper.get(".common-metrics h2").text()).toBe("常用指标");
  expect(wrapper.get(".diagnostic-metrics summary").text()).toContain(
    "诊断指标",
  );
  wrapper.unmount();

  const events = mount(Events, {
    props: {
      events: [
        {
          time: "2026-10-02T08:00:00Z",
          resource: "claude-metadata:latest",
          category: "metadata",
          message: "Upstream HTTP 502",
          status_code: 502,
        },
      ],
    },
  });
  expect(events.get("tbody").text()).toContain("claude-metadata:latest");
  expect(events.get(".diagnostic").text()).toBe("Upstream HTTP 502");
  expect(events.get(".count-badge").text()).toBe("1");
  events.unmount();
});
