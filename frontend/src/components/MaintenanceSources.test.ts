import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import Maintenance from "./Maintenance.vue";
import { response } from "../testSupport";

afterEach(() => { vi.unstubAllGlobals(); });
it("scopes release cleanup to a historical source, keeps TTL current and permits cleanup after deletion", async () => {
  let delay = false, resolvePreview: ((value: unknown) => void) | undefined;
  const job = { job: { ID: "release-history", Selected: [] }, logical_bytes: 0, reclaimable_blob_bytes: 0, active: 0, unknown_versions: [] };
  const fetch = vi.fn((url: string, init?: RequestInit) => {
    const path = new URL(url, "https://admin.example").pathname;
    if (path.endsWith("/sources")) return Promise.resolve(response({ sources: [
      { epoch: 1, base_url: "https://old.example/releases", current: false, active: false, created_at: "2026-09-01T00:00:00Z" },
      { epoch: 2, base_url: "https://current.example/releases", current: true, active: true, created_at: "2026-10-01T00:00:00Z" },
    ] }));
    if (path.endsWith("/settings")) return Promise.resolve(response({ channel_ttl_seconds: 60, revision: 1 }));
    if (path.endsWith("/preview")) return delay ? new Promise((resolve) => { resolvePreview = resolve; }) : Promise.resolve(response(job));
    return Promise.resolve(response({ ok: true }));
  });
  vi.stubGlobal("fetch", fetch);
  const wrapper = mount(Maintenance, { props: { application: "openai/codex" } });
  await flushPromises();
  await wrapper.get('[name="source_epoch"]').setValue("1");
  await wrapper.get(".ttl-form input").setValue("120");
  await wrapper.get(".ttl-form").trigger("submit"); await flushPromises();
  const ttl = fetch.mock.calls.find(([, init]) => init?.method === "PUT")!;
  expect(ttl[0]).toBe("/admin/api/apps/openai/codex/settings");
  await wrapper.get(".cleanup input").setValue("1.2.3");
  await wrapper.get(".cleanup").trigger("submit"); await flushPromises();
  expect(fetch.mock.calls.at(-1)![0]).toBe("/admin/api/apps/openai/codex/cleanup/preview?source_epoch=1");
  await wrapper.get(".cleanup-review .danger").trigger("click"); await flushPromises();
  expect(fetch.mock.calls.at(-1)![0]).toBe("/admin/api/apps/openai/codex/cleanup/release-history/execute?source_epoch=1");
  delay = true;
  await wrapper.get(".cleanup").trigger("submit");
  const request = fetch.mock.calls.at(-1)![1]!;
  await wrapper.get('[name="source_epoch"]').setValue("");
  expect(request.signal?.aborted).toBe(true);
  resolvePreview?.(response(job)); await flushPromises();
  expect(wrapper.find(".cleanup-review").exists()).toBe(false);
  await wrapper.setProps({ showSettings: false }); await flushPromises();
  expect(wrapper.find(".ttl-form").exists()).toBe(false);
  expect(wrapper.find(".cleanup").exists()).toBe(true);
  wrapper.unmount();
});
