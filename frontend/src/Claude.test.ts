import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import Resources from "./components/Resources.vue";
import Maintenance from "./components/Maintenance.vue";
import { response } from "./testSupport";
import { setLanguage } from "./i18n";
import { signedIn } from "./session";
afterEach(() => {
  vi.unstubAllGlobals();
  signedIn.value = false;
  vi.useRealTimers();
  setLanguage("en");
});
it("paginates scoped resources, retries errors, resets server filtering, and discards a late previous-app response", async () => {
  signedIn.value = true;
  let fail = true,
    pending = false,
    resolveOld: ((value: unknown) => void) | undefined;
  const resource = (app: string, key: string) => ({
    ID: key,
    State: "complete",
    Resource: { Application: app, Version: "1.0.0", Key: key, Labels: {} },
    VerificationNS: 0,
  });
  const fetch = vi.fn((url: string) => {
    const query = new URL(url, "https://test.example");
    const app = url.includes("anthropic/")
      ? "anthropic/claude-code"
      : "openai/codex";
    if (query.pathname.endsWith("/versions"))
      return Promise.resolve(
        response({
          items: [
            {
              version: "1.0.0",
              first_seen: "2026-01-01",
              requests: 7,
              bytes: 0,
            },
          ],
          next_cursor: null,
        }),
      );
    if (pending && app === "anthropic/claude-code")
      return new Promise((r) => {
        resolveOld = r;
      });
    if (fail)
      return Promise.resolve(
        response(
          {
            error: {
              code: "LOCAL_STORAGE_UNAVAILABLE",
              message: "storage failed",
            },
          },
          503,
        ),
      );
    return Promise.resolve(
      response({
        items: query.searchParams.has("version")
          ? []
          : [
              resource(
                app,
                query.searchParams.has("cursor")
                  ? "second-resource"
                  : `${app}-file`,
              ),
            ],
        next_cursor:
          query.searchParams.has("version") || query.searchParams.has("cursor")
            ? null
            : "cursor/+==",
      }),
    );
  });
  vi.stubGlobal("fetch", fetch);
  const wrapper = mount(Resources, {
    props: { application: "anthropic/claude-code" },
  });
  await flushPromises();
  expect(wrapper.text()).toContain("7 artifact requests");
  expect(wrapper.text()).toContain("Local storage is unavailable");
  expect(wrapper.text()).not.toContain("No cached resources");
  fail = false;
  await wrapper.find("[role=alert] button").trigger("click");
  await flushPromises();
  expect(wrapper.text()).toContain("anthropic/claude-code-file");
  const nav = () => wrapper.find('[aria-label="Resource pages"]');
  await nav()
    .findAll("button")
    .find((b) => b.text() === "Next page")!
    .trigger("click");
  await flushPromises();
  expect(wrapper.text()).toContain("second-resource");
  expect(
    new URL(
      fetch.mock.calls.at(-1)![0],
      "https://test.example",
    ).searchParams.get("cursor"),
  ).toBe("cursor/+==");
  await nav()
    .findAll("button")
    .find((b) => b.text() === "Previous page")!
    .trigger("click");
  await flushPromises();
  expect(wrapper.text()).toContain("anthropic/claude-code-file");
  await wrapper.find(".version-button").trigger("click");
  await flushPromises();
  const filtered = new URL(fetch.mock.calls.at(-1)![0], "https://test.example");
  expect(filtered.searchParams.get("version")).toBe("1.0.0");
  expect(filtered.searchParams.has("cursor")).toBe(false);
  expect(wrapper.text()).toContain("No cached resources");
  pending = true;
  await nav()
    .findAll("button")
    .find((b) => b.text() === "Refresh")!
    .trigger("click");
  await wrapper.setProps({ application: "openai/codex" });
  await flushPromises();
  resolveOld?.(
    response({
      items: [resource("anthropic/claude-code", "late-old-app")],
      next_cursor: "old-cursor",
    }),
  );
  await flushPromises();
  expect(wrapper.text()).toContain("openai/codex-file");
  expect(wrapper.text()).not.toContain("late-old-app");
  expect(wrapper.find("[role=combobox]").text()).toContain("All versions");
  expect(
    fetch.mock.calls.every(
      ([url]) =>
        new URL(url, "https://test.example").searchParams.get("limit") === "50",
    ),
  ).toBe(true);
  wrapper.unmount();
});
it("clears TTL on failed app switch, rejects late results, and binds cleanup to app and frozen preview", async () => {
  let resolveLoad: ((value: unknown) => void) | undefined,
    resolvePreview: ((value: unknown) => void) | undefined,
    resolveExecute: ((value: unknown) => void) | undefined;
  let delay = false,
    fail = false;
  const fetch = vi.fn((url: string, init?: RequestInit) =>
    url.endsWith("/sources")
      ? Promise.resolve(response({ sources: [] }))
      : url.endsWith("/settings")
      ? fail
        ? Promise.resolve(response({}, 503))
        : delay
          ? new Promise((r) => {
              resolveLoad = r;
            })
          : Promise.resolve(response({ channel_ttl_seconds: 60, revision: 0 }))
      : url.endsWith("/preview")
        ? new Promise((r) => {
            resolvePreview = r;
          })
        : new Promise((r) => {
            resolveExecute = r;
          }),
  );
  vi.stubGlobal("fetch", fetch);
  const wrapper = mount(Maintenance, {
    props: { application: "openai/codex" },
  });
  await flushPromises();
  fail = true;
  await wrapper.setProps({ application: "anthropic/claude-code" });
  await flushPromises();
  expect(
    (wrapper.find("input[type=number]").element as HTMLInputElement).value,
  ).toBe("");
  expect(wrapper.find(".ttl-form button").attributes("disabled")).toBeDefined();
  fail = false;
  delay = true;
  await wrapper.setProps({ application: "openai/codex" });
  delay = false;
  await wrapper.setProps({ application: "anthropic/claude-code" });
  await flushPromises();
  resolveLoad?.(response({ channel_ttl_seconds: 999, revision: 0 }));
  await flushPromises();
  expect(
    (wrapper.find("input[type=number]").element as HTMLInputElement).value,
  ).toBe("60");
  await wrapper.find(".cleanup input").setValue("1.0.0");
  await wrapper.find(".cleanup").trigger("submit");
  await wrapper.find(".cleanup input").setValue("2.0.0");
  resolvePreview?.(
    response({
      job: { ID: "stale", Selected: [] },
      logical_bytes: 0,
      reclaimable_blob_bytes: 0,
      active: 0,
    }),
  );
  await flushPromises();
  expect(wrapper.find(".cleanup-review").exists()).toBe(false);
  await wrapper.find(".cleanup").trigger("submit");
  resolvePreview?.(
    response({
      job: {
        ID: "frozen",
        Selected: [
          { Resource: "resource-1", Generation: "generation-1" },
          { Resource: "resource-2", Generation: "generation-2" },
        ],
      },
      logical_bytes: 4096,
      reclaimable_blob_bytes: 1024,
      active: 1,
    }),
  );
  await flushPromises();
  expect(wrapper.find(".cleanup-review").text()).toContain(
    "2 generations · 4.00 KiB logical bytes · 1 active",
  );
  expect(wrapper.find(".cleanup-reclaimable").text()).toBe(
    "Estimated reclaimable complete cache: 1.00 KiB",
  );
  setLanguage("zh-CN");
  await flushPromises();
  expect(wrapper.find(".cleanup-reclaimable").text()).toBe(
    "预计可回收完整缓存：1.00 KiB",
  );
  await wrapper.find(".cleanup-review .danger").trigger("click");
  await wrapper.find(".cleanup-review .danger").trigger("click");
  expect(
    fetch.mock.calls.filter(([url]) => url.endsWith("/execute")),
  ).toHaveLength(1);
  expect(fetch.mock.calls.at(-1)![0]).toBe(
    "/admin/api/apps/anthropic/claude-code/cleanup/frozen/execute",
  );
  resolveExecute?.(response({ ok: true }));
  await flushPromises();
  expect(wrapper.find(".cleanup-review").exists()).toBe(false);
  wrapper.unmount();
});
