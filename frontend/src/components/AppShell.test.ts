import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import AppShell from "./AppShell.vue";
import { applySite, defaultSite } from "../site";
import { language, setLanguage } from "../i18n";
import { bootstrap, loadBootstrap } from "../bootstrap";
import { boot, resetStores, response } from "../testSupport";
beforeEach(resetStores);
afterEach(() => {
  vi.unstubAllGlobals();
  applySite(defaultSite);
  setLanguage("en");
});
it("uses translated site text safely, fixed source link, and separate public/admin version formats", async () => {
  bootstrap.value = boot;
  const site = structuredClone(defaultSite);
  site.title.en = "<img src=x onerror=alert(1)>";
  site.disclaimer.en = "<script>alert(1)</script>";
  applySite(site);
  const w = mount(AppShell, {
    global: { stubs: { RouterLink: { template: "<a><slot/></a>" } } },
  });
  expect(w.get(".brand").text()).toContain(site.title.en);
  expect(w.find("img,script").exists()).toBe(false);
  expect(w.get("footer a").attributes("href")).toBe(
    "https://github.com/PMExtra/RedApp",
  );
  expect(w.get("footer").text()).not.toContain("arm64");
  setLanguage("zh-CN");
  await w.vm.$nextTick();
  expect(w.get(".brand").text()).toContain("应用再分发平台");
  await w.setProps({ admin: true });
  expect(w.get("footer").text()).toContain("vdev (linux/arm64)");
  w.unmount();
});
it("does not overwrite a newly saved site with a late bootstrap response", async () => {
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
  const promise = loadBootstrap();
  const fresh = structuredClone(defaultSite);
  fresh.title.en = "Newly saved";
  applySite(fresh);
  resolve?.(response(boot));
  await promise;
  await flushPromises();
  const w = mount(AppShell, {
    global: { stubs: { RouterLink: { template: "<a><slot/></a>" } } },
  });
  expect(w.get(".brand").text()).toContain("Newly saved");
  w.unmount();
});

it.each([false, true])('keeps decorative En/Zh icons, full names and selection in the shared language menu (admin=%s)', async (admin) => {
  resetStores();
  const w = mount(AppShell, { props: { admin }, global: { stubs: { RouterLink: { template: '<a><slot /></a>' } } } });
  for (const locale of ['en', 'zh-CN'] as const) {
    setLanguage(locale); await flushPromises();
    await w.get('.language-control button').trigger('click');
    const options = w.findAll('.language-control [role=option]');
    expect(options.map(o => o.get('.select-option-text-icon').text())).toEqual(['En','Zh']);
    expect(options.map(o => o.get('.select-option-label').text())).toEqual(['English','简体中文']);
    for (const option of options) {
      expect(option.get('.select-option-text-icon').attributes('aria-hidden')).toBe('true');
      expect(option.find('img').exists()).toBe(false);
      expect(option.text().includes('✓')).toBe(option.attributes('data-value') === locale);
    }
    const other = locale === 'en' ? 'zh-CN' : 'en';
    await w.get(`[data-value="${other}"]`).trigger('click');
    expect(language.value).toBe(other);
    expect(w.find('[role=listbox]').exists()).toBe(false);
  }
  w.unmount();
});
