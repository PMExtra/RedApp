import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import RetentionSettings from "./RetentionSettings.vue";
import { configurationFixture, response, resetStores } from "../testSupport";
import { setLanguage } from "../i18n";
afterEach(() => {
  vi.unstubAllGlobals();
  setLanguage("en");
});
it("uses sparse whole policy, confirms enabling, previews and executes with paging", async () => {
  resetStores();
  let config = configurationFixture(
    { retention: { enabled: false, keep_latest: 3 } },
    3,
    "openai/codex",
  );
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("/configuration")) {
      if (init?.method === "PATCH") {
        const patch = JSON.parse(init.body as string);
        expect(patch).toEqual({
          revision: 3,
          set: { retention: { enabled: true, keep_latest: 1 } },
          unset: [],
        });
        config = { ...config, revision: 4 };
        (config.effective as Record<string, unknown>).retention = {
          enabled: true,
          keep_latest: 1,
        };
      }
      return response(config);
    }
    if (url.endsWith("/preview"))
      return response({
        id: "preview",
        selected_versions: 1,
        logical_bytes: 10,
        reclaimable_bytes: 10,
      });
    if (url.includes("/items"))
      return response({
        items: [
          {
            version: "1.0.0",
            reasons: ["outside_latest_n"],
            bytes: 10,
            selected: true,
          },
        ],
        total: 1,
        total_pages: 1,
      });
    if (url.endsWith("/execute"))
      return response({ retired_versions: 1, logical_bytes: 10, skipped: {} });
    return response({});
  });
  vi.stubGlobal("fetch", fetch);
  const w = mount(RetentionSettings, {
    props: { application: "openai/codex" },
  });
  await flushPromises();
  await w.get("form").trigger("submit");
  expect(
    fetch.mock.calls.filter(([, i]) => i?.method === "PATCH"),
  ).toHaveLength(0);
  await w.get("input[type=checkbox]").setValue(true);
  expect(w.text()).toContain("Enable automatic deletion");
  expect(
    fetch.mock.calls.filter(([, i]) => i?.method === "PATCH"),
  ).toHaveLength(0);
  await w
    .findAll("button")
    .find((b) => b.text() === "Enable automatic cleanup")!
    .trigger("click");
  await w.get("input[type=number]").setValue("1");
  await w.get("form").trigger("submit");
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "Enable automatic cleanup")!
    .trigger("click");
  await flushPromises();
  expect(
    fetch.mock.calls.filter(([, i]) => i?.method === "PATCH"),
  ).toHaveLength(1);
  await w
    .findAll("button")
    .find((b) => b.text() === "Preview cleanup")!
    .trigger("click");
  await flushPromises();
  expect(w.get("tbody").text()).toContain("1.0.0");
  expect(
    JSON.parse(
      fetch.mock.calls.find(([u]) => u.endsWith("/preview"))![1]!
        .body as string,
    ),
  ).toEqual({ revision: 4 });
  await w
    .findAll("button")
    .find((b) => b.text() === "Clean now using this policy")!
    .trigger("click");
  await flushPromises();
  expect(w.text()).toContain("Cleanup executed");
  w.unmount();
});
it("cancels stale preview on application switch and preserves a 409 draft", async () => {
  resetStores();
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>((r) => {
    resolve = r;
  });
  const pending = { promise, resolve };
  const config = configurationFixture(
    { retention: { enabled: false, keep_latest: 3 } },
    3,
    "openai/codex",
  );
  let signal: AbortSignal | undefined;
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("/preview")) {
      signal = init?.signal as AbortSignal;
      return pending.promise;
    }
    if (init?.method === "PATCH") return response({ error: "conflict" }, 409);
    if (url.endsWith("/configuration")) return response(config);
    return response({});
  });
  vi.stubGlobal("fetch", fetch);
  const w = mount(RetentionSettings, {
    props: { application: "openai/codex" },
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "Preview cleanup")!
    .trigger("click");
  await w.setProps({ application: "anthropic/claude-code" });
  await flushPromises();
  expect(signal?.aborted).toBe(true);
  pending.resolve(
    response({
      id: "old",
      selected_versions: 1,
      logical_bytes: 10,
    }) as Response,
  );
  await flushPromises();
  expect(w.find("tbody").exists()).toBe(false);
  await w.get("input[type=number]").setValue("7");
  await w.get("form").trigger("submit");
  await flushPromises();
  expect((w.get("input[type=number]").element as HTMLInputElement).value).toBe(
    "7",
  );
  expect(w.find("[role=alert]").exists()).toBe(true);
  w.unmount();
});

it("restores the whole template policy and invalidates a preview when N changes", async () => {
  resetStores();
  const config = configurationFixture(
    { retention: { enabled: false, keep_latest: 5 } },
    3,
    "openai/codex",
  );
  config.fields.retention = { source: "custom", differs_from_template: true };
  (config.defaults as Record<string, unknown>).retention = {
    enabled: false,
    keep_latest: 3,
  };
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("/configuration")) return response(config);
    if (url.endsWith("/preview"))
      return response({
        id: "preview",
        selected_versions: 1,
        logical_bytes: 10,
        reclaimable_bytes: 10,
      });
    if (url.includes("/items"))
      return response({ items: [], total: 0, total_pages: 1 });
    return response({});
  });
  vi.stubGlobal("fetch", fetch);
  const w = mount(RetentionSettings, {
    props: { application: "openai/codex" },
  });
  await flushPromises();
  await w
    .findAll("button")
    .find((b) => b.text() === "Preview cleanup")!
    .trigger("click");
  await flushPromises();
  expect(w.find("table").exists()).toBe(true);
  await w.get("input[type=number]").setValue("2");
  expect(w.find("table").exists()).toBe(false);
  await w.get(".override-control button").trigger("click");
  expect((w.get("input[type=number]").element as HTMLInputElement).value).toBe(
    "3",
  );
  await w.get("form").trigger("submit");
  await flushPromises();
  const call = fetch.mock.calls.find(([, init]) => init?.method === "PATCH")!;
  expect(JSON.parse(call[1]!.body as string)).toEqual({
    revision: 3,
    set: {},
    unset: ["retention"],
  });
  w.unmount();
});
