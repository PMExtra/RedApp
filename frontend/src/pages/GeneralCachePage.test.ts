import { selectValue } from "../testSupport";
import { flushPromises } from "@vue/test-utils";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  adminApplications,
  managedVendors,
  boot,
  mountPage,
  resetStores,
  response,
} from "../testSupport";
import { setLanguage } from "../i18n";

const apps = [
  {
    ...adminApplications[0]!,
    provider: "http-cache",
    key: "openai/files",
    id: "files",
    name: { en: "Files", "zh-CN": "文件" },
  },
  {
    ...adminApplications[0]!,
    uid: "other-files",
    provider: "http-cache",
    key: "openai/other",
    id: "other",
    name: { en: "Other files", "zh-CN": "其他文件" },
  },
];
const row = {
  generation_id: "generation-1",
  path: "releases/tool.zip",
  size_bytes: 4096,
  sha256: "a".repeat(64),
  fetched_at: "2026-10-01T08:00:00Z",
  validated_at: "2026-10-02T08:00:00Z",
  last_access_at: "2026-10-02T12:00:00Z",
  fresh_until: "2026-10-02T08:05:00Z",
  etag: '"first"',
  source_url: "https://actual.example/releases/tool.zip",
};
const sources = [
  {
    epoch: 1,
    base_url: "https://old.example/files/",
    current: false,
    active: false,
    created_at: "2026-09-01T00:00:00Z",
  },
  {
    epoch: 2,
    base_url: "https://current.example/files/",
    current: true,
    active: true,
    created_at: "2026-10-01T00:00:00Z",
  },
];
function common(url: string) {
  if (url === "/api/bootstrap") return response(boot);
  const app = apps.find((item) => url === `/admin/api/apps/${item.key}`);
  if (app) return response({ app });
  const vendor = managedVendors.find(
    (item) => url === `/admin/api/vendors/${item.id}`,
  );
  if (vendor) return response({ vendor });
  if (url.endsWith("/session")) return response({ csrf: "cache-token" });
  if (url === "/admin/api/apps") return response({ apps });
  if (url === "/admin/api/vendors")
    return response({ vendors: managedVendors });
  if (url.endsWith("/sources")) return response({ sources });
  if (new URL(url, "https://test.example").pathname.endsWith("/items"))
    return response({
      items: [
        {
          ordinal: 0,
          generation_id: row.generation_id,
          path: row.path,
          size_bytes: row.size_bytes,
          result_status: "pending",
        },
      ],
      next_cursor: null,
      total_files: 4,
      total_bytes: 8192,
      state: "ready",
    });
  return response({ items: [row] });
}
function preview(body: unknown, id = "frozen-preview") {
  return response({
    job: {
      ...(body as object),
      id,
      kind: "cleanup",
      state: "ready",
      created_at: "2026-10-03T00:00:00Z",
      scanned_files: 4,
      completed_files: 0,
      failed_files: 0,
      expires_at: "2099-10-03T23:00:00Z",
      selected_files: 4,
      selected_bytes: 8192,
      active_files: 1,
    },
  });
}
beforeEach(resetStores);
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  setLanguage("en");
  document.body.innerHTML = "";
});

it("uses the selected time basis and local cutoff, then executes only the frozen preview and explains skipped files", async () => {
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("/cleanup/preview"))
      return preview(JSON.parse(init!.body as string));
    if (url.endsWith("/execute"))
      return response({
        result: {
          selected_files: 4,
          retired_files: 2,
          skipped_accessed: 1,
          skipped_changed: 1,
          retired_bytes: 4096,
        },
      });
    return common(url);
  });
  vi.stubGlobal("fetch", fetch);
  const { wrapper } = await mountPage("/admin/vendors/openai/apps/files/cache");
  expect(wrapper.get(".cache-table").text()).toContain("releases/tool.zip");
  expect(wrapper.get(".cache-table").text()).toContain("4.00 KiB");
  expect(wrapper.get(".cache-table").text()).toContain("Actual source");
  expect(wrapper.get(".cache-table").text()).toContain(
    "https://actual.example/releases/tool.zip",
  );
  expect(wrapper.get(".time-cleanup").text()).toContain(
    "still frequently accessed",
  );
  await selectValue(wrapper.get('[name="basis"]'), "last_access");
  expect(wrapper.get(".time-cleanup").text()).toContain(
    "checked again and skipped",
  );
  await wrapper.get('[name="before"]').setValue("2026-10-03T15:45");
  const offset = new Date(2026, 9, 3, 15, 45).getTimezoneOffset();
  const expectedUTC = new Date(
    Date.UTC(2026, 9, 3, 15, 45) + offset * 60000,
  ).toISOString();
  expect(wrapper.get(".cleanup-cutoff time").text()).toBe(expectedUTC);
  await wrapper.get(".time-cleanup form").trigger("submit");
  await flushPromises();
  const request = fetch.mock.calls.find(([url]) =>
    url.endsWith("/cleanup/preview"),
  )![1]!;
  expect(JSON.parse(request.body as string)).toEqual({
    match: { type: "glob", pattern: "/" },
    basis: "last_access",
    before: expectedUTC,
  });
  expect(wrapper.get(".cleanup-review").text()).toContain(
    "4 files · 8.00 KiB logical bytes · 1 active",
  );
  expect(fetch.mock.calls.some(([url]) => url.endsWith("/execute"))).toBe(
    false,
  );
  await wrapper.get(".cleanup-review .danger").trigger("click");
  await wrapper.get(".cleanup-review .danger").trigger("click");
  await flushPromises();
  expect(
    fetch.mock.calls.filter(([url]) => url.endsWith("/execute")),
  ).toHaveLength(1);
  expect(fetch.mock.calls.find(([url]) => url.endsWith("/execute"))![0]).toBe(
    "/admin/api/apps/openai/files/cache/cleanup/frozen-preview/execute",
  );
  expect(wrapper.get(".cleanup-result").text()).toContain(
    "Retired 2 of 4 files · 4.00 KiB logical bytes",
  );
  expect(wrapper.get(".cleanup-result").text()).toContain(
    "1 accessed since preview; 1 changed generations",
  );
  expect(wrapper.find(".cleanup-review .danger").exists()).toBe(false);
  expect(wrapper.find(".cleanup-review .preview-files").exists()).toBe(true);
  wrapper.unmount();
});

it("preserves a failed cleanup across automatic list refreshes and invalidates a changed preview selection", async () => {
  vi.useFakeTimers();
  let fail = true,
    resolvePreview: ((value: unknown) => void) | undefined;
  const fetch = vi.fn((url: string, init?: RequestInit) => {
    if (url.endsWith("/cleanup/preview")) {
      if (fail) return Promise.resolve(response({}, 503));
      return new Promise((resolve) => {
        resolvePreview = resolve;
      });
    }
    return Promise.resolve(common(url));
  });
  vi.stubGlobal("fetch", fetch);
  const { wrapper } = await mountPage("/admin/vendors/openai/apps/files/cache");
  await wrapper.get('[name="before"]').setValue("2026-10-03T15:45");
  await wrapper.get(".time-cleanup form").trigger("submit");
  await flushPromises();
  expect(wrapper.get(".cleanup-error").text()).toContain(
    "Service temporarily unavailable",
  );
  const count = fetch.mock.calls.filter(([url]) =>
    url.endsWith("/cache"),
  ).length;
  await vi.advanceTimersByTimeAsync(5000);
  await flushPromises();
  expect(
    fetch.mock.calls.filter(([url]) => url.endsWith("/cache")),
  ).toHaveLength(count + 1);
  expect(wrapper.get(".cleanup-error").text()).toContain(
    "Service temporarily unavailable",
  );
  fail = false;
  await wrapper.get(".time-cleanup form").trigger("submit");
  const request = fetch.mock.calls.at(-1)![1]!;
  await wrapper
    .get('.time-cleanup [name="match_pattern"]')
    .setValue("/archive/");
  expect(request.signal?.aborted).toBe(true);
  resolvePreview?.(
    preview(JSON.parse(request.body as string), "old-selection"),
  );
  await flushPromises();
  expect(wrapper.find(".cleanup-review").exists()).toBe(false);
  expect(
    wrapper.get(".time-cleanup form button").attributes("disabled"),
  ).toBeUndefined();
  wrapper.unmount();
});

it("discards late cache and cleanup responses when changing applications", async () => {
  let delayCache = false,
    delaySources = false,
    resolveSources: ((value: unknown) => void) | undefined,
    resolveCache: ((value: unknown) => void) | undefined,
    resolvePreview: ((value: unknown) => void) | undefined;
  const fetch = vi.fn((url: string, init?: RequestInit) => {
    if (delaySources && url === "/admin/api/apps/openai/files/sources")
      return new Promise((resolve) => {
        resolveSources = resolve;
      });
    if (url === "/admin/api/apps/openai/other/sources")
      return Promise.resolve(
        response({
          sources: [
            { ...sources[1], epoch: 3, base_url: "https://other.example/" },
          ],
        }),
      );
    if (url.endsWith("/cleanup/preview"))
      return new Promise((resolve) => {
        resolvePreview = resolve;
      });
    if (delayCache && url === "/admin/api/apps/openai/files/cache")
      return new Promise((resolve) => {
        resolveCache = resolve;
      });
    if (url === "/admin/api/apps/openai/other/cache")
      return Promise.resolve(
        response({ items: [{ ...row, path: "other-current.zip" }] }),
      );
    return Promise.resolve(common(url));
  });
  vi.stubGlobal("fetch", fetch);
  const { wrapper, router } = await mountPage(
    "/admin/vendors/openai/apps/files/cache",
  );
  await selectValue(wrapper.get('[name="basis"]'), "last_access");
  await wrapper.get('[name="before"]').setValue("2026-10-03T15:45");
  await wrapper.get(".time-cleanup form").trigger("submit");
  const request = fetch.mock.calls.at(-1)![1]!;
  delayCache = true;
  await wrapper.get(".cache-refresh").trigger("click");
  const listRequest = fetch.mock.calls.at(-1)![1]!;
  delaySources = true;
  await wrapper.get(".source-epoch-select button").trigger("click");
  const sourcesRequest = fetch.mock.calls.at(-1)![1]!;
  await router.push("/admin/vendors/openai/apps/other/cache");
  await flushPromises();
  expect(request.signal?.aborted).toBe(true);
  expect(listRequest.signal?.aborted).toBe(true);
  expect(sourcesRequest.signal?.aborted).toBe(true);
  resolveSources?.(
    response({
      sources: [
        { ...sources[0], epoch: 99, base_url: "https://wrong-app.example/" },
      ],
    }),
  );
  resolveCache?.(response({ items: [{ ...row, path: "late-wrong-app.zip" }] }));
  resolvePreview?.(
    preview(JSON.parse(request.body as string), "wrong-app-preview"),
  );
  await flushPromises();
  expect(wrapper.get(".cache-table").text()).toContain("other-current.zip");
  expect(wrapper.text()).not.toContain("late-wrong-app.zip");
  expect(wrapper.get('[name="source_epoch"]').text()).toContain(
    "https://other.example/",
  );
  expect(wrapper.get('[name="source_epoch"]').text()).not.toContain(
    "wrong-app.example",
  );
  expect(wrapper.find(".cleanup-review").exists()).toBe(false);
  expect(
    (wrapper.get('[name="before"]').element as HTMLInputElement).value,
  ).toBe("");
  expect(wrapper.get('[name="basis"] [role=combobox]').text()).toBe(
    "Fetched at",
  );
  wrapper.unmount();
});

it("binds historical cache listing, preview and execution to the selected source and cancels its pending preview on source change", async () => {
  let delayed = false,
    resolveOld: ((value: unknown) => void) | undefined;
  const fetch = vi.fn((url: string, init?: RequestInit) => {
    const path = new URL(url, "https://admin.example").pathname;
    if (path.endsWith("/cleanup/preview")) {
      if (delayed)
        return new Promise((resolve) => {
          resolveOld = resolve;
        });
      return Promise.resolve(
        preview(JSON.parse(init!.body as string), "historical-preview"),
      );
    }
    if (path.endsWith("/execute"))
      return Promise.resolve(
        response({
          result: {
            selected_files: 4,
            retired_files: 4,
            skipped_accessed: 0,
            skipped_changed: 0,
            retired_bytes: 8192,
          },
        }),
      );
    return Promise.resolve(common(url));
  });
  vi.stubGlobal("fetch", fetch);
  const { wrapper } = await mountPage("/admin/vendors/openai/apps/files/cache");
  const selector = wrapper.get('[name="source_epoch"]');
  await selector.get("[role=combobox]").trigger("click");
  expect(selector.text()).toContain(
    "Source 1 · https://old.example/files/ · Historical source",
  );
  expect(selector.text()).toContain(
    "Source 2 · https://current.example/files/ · Current source",
  );
  await selectValue(selector, "1");
  await flushPromises();
  expect(wrapper.find(".cache-refresh-panel").exists()).toBe(false);
  expect(wrapper.get(".refresh-file").attributes("disabled")).toBeDefined();
  expect(
    fetch.mock.calls.some(
      ([url]) => url === "/admin/api/apps/openai/files/cache?source_epoch=1",
    ),
  ).toBe(true);
  await wrapper.get('[name="before"]').setValue("2026-10-03T15:45");
  await wrapper.get(".time-cleanup form").trigger("submit");
  await flushPromises();
  expect(
    fetch.mock.calls.some(
      ([url]) =>
        url ===
        "/admin/api/apps/openai/files/cache/cleanup/preview?source_epoch=1",
    ),
  ).toBe(true);
  await wrapper.get(".cleanup-review .danger").trigger("click");
  await flushPromises();
  expect(
    fetch.mock.calls.some(
      ([url]) =>
        url ===
        "/admin/api/apps/openai/files/cache/cleanup/historical-preview/execute?source_epoch=1",
    ),
  ).toBe(true);
  expect(wrapper.find(".cleanup-result").exists()).toBe(true);
  delayed = true;
  await wrapper.get(".time-cleanup form").trigger("submit");
  const request = fetch.mock.calls.at(-1)![1]!;
  await selectValue(selector, "");
  await flushPromises();
  expect(request.signal?.aborted).toBe(true);
  resolveOld?.(preview(JSON.parse(request.body as string), "late-source"));
  await flushPromises();
  expect(wrapper.find(".cleanup-review").exists()).toBe(false);
  expect(wrapper.find(".cleanup-result").exists()).toBe(false);
  expect(fetch.mock.calls.at(-1)![0]).toBe(
    "/admin/api/apps/openai/files/cache",
  );
  wrapper.unmount();
});
