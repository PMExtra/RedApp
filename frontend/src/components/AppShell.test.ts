import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import AppShell from "./AppShell.vue";
import { applySite, defaultSite } from "../site";
import { setLanguage } from "../i18n";
afterEach(() => {
  vi.unstubAllGlobals();
  applySite(defaultSite);
  setLanguage("en");
});
it("uses bilingual defaults and a fixed project link with separate public/admin footer formats", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => ({
      ok: true,
      json: async () => ({ version: "0.4.1", os: "linux", arch: "arm64" }),
    })),
  );
  const w = mount(AppShell);
  await flushPromises();
  expect(w.get(".brand").text()).toContain(
    "Application Redistribution Platform",
  );
  expect(w.get("footer").text()).not.toContain("arm64");
  expect(w.get("footer a").text()).toBe("RedApp");
  expect(w.get("footer a").attributes("href")).toBe(
    "https://github.com/PMExtra/RedApp",
  );
  expect(w.classes()).toContain("public-shell");
  setLanguage("zh-CN");
  await w.vm.$nextTick();
  expect(w.get(".brand").text()).toContain("应用再分发平台");
  expect(w.findAll(".footer-notice")).toHaveLength(1);
  await w.setProps({ admin: true });
  expect(w.get("footer").text()).toContain("v0.4.1 (linux/arm64)");
  expect(w.get("footer").text()).not.toContain("架构");
  w.unmount();
});
it("renders configured text as text and never changes the project source link", async () => {
  const site = structuredClone(defaultSite);
  site.title.en = "<img src=x onerror=alert(1)>";
  site.disclaimer.en = "<script>alert(1)</script>";
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => ({
      ok: true,
      json: async () => ({
        version: "0.4.1",
        os: "linux",
        arch: "amd64",
        site,
      }),
    })),
  );
  const w = mount(AppShell);
  await flushPromises();
  expect(w.get(".brand").text()).toContain(site.title.en);
  expect(w.find("img,script").exists()).toBe(false);
  expect(w.get(".footer-notice").text()).toBe(site.disclaimer.en);
  expect(w.get("footer a").attributes("href")).toBe(
    "https://github.com/PMExtra/RedApp",
  );
  w.unmount();
});

it("does not overwrite a newly saved site with a late initial response", async () => {
  let resolve: ((value: unknown) => void) | undefined;
  vi.stubGlobal(
    "fetch",
    vi.fn(
      () =>
        new Promise((r) => {
          resolve = r;
        }),
    ),
  );
  const w = mount(AppShell);
  const fresh = structuredClone(defaultSite);
  fresh.title.en = "Newly saved";
  applySite(fresh);
  resolve?.({
    ok: true,
    json: async () => ({
      version: "0.4.1",
      os: "linux",
      arch: "amd64",
      site: defaultSite,
    }),
  });
  await flushPromises();
  expect(w.get(".brand").text()).toContain("Newly saved");
  w.unmount();
});
