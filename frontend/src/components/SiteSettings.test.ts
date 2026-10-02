import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import SiteSettings from "./SiteSettings.vue";
import { applySite, defaultSite, siteSettings } from "../site";
afterEach(() => {
  vi.unstubAllGlobals();
  applySite(defaultSite);
});
it("loads bilingual settings, saves once, updates public chrome only after success and handles errors", async () => {
  let resolve: ((value: unknown) => void) | undefined;
  let failed = false;
  const fetch = vi.fn(async (_url: string, init?: RequestInit) =>
    init?.method === "POST"
      ? failed
        ? {
            ok: false,
            status: 503,
            json: async () => ({ error: "Unavailable" }),
          }
        : new Promise((r) => {
            resolve = r;
          })
      : { ok: true, json: async () => structuredClone(defaultSite) },
  );
  vi.stubGlobal("fetch", fetch);
  const w = mount(SiteSettings);
  await flushPromises();
  expect(w.findAll("input")).toHaveLength(4);
  expect(w.findAll("textarea")).toHaveLength(2);
  await w.find("input").setValue("Internal tools");
  await w.find("form").trigger("submit");
  await w.find("form").trigger("submit");
  expect(fetch.mock.calls.filter((c) => c[1]?.method === "POST")).toHaveLength(
    1,
  );
  expect(siteSettings.value.title.en).toBe("RedApp");
  const saved = structuredClone(defaultSite);
  saved.title.en = "Internal tools";
  resolve?.({ ok: true, json: async () => saved });
  await flushPromises();
  expect(siteSettings.value.title.en).toBe("Internal tools");
  expect(w.text()).toContain("Site settings saved.");
  failed = true;
  await w.find("input").setValue("Unsaved");
  await w.find("form").trigger("submit");
  await flushPromises();
  expect(w.emitted("error")).toHaveLength(1);
  expect(siteSettings.value.title.en).toBe("Internal tools");
  w.unmount();
});
