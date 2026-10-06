import { readFileSync } from "node:fs";
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import VendorCard from "./VendorCard.vue";
import { adminApplications, managedVendors, response } from "../testSupport";

const links = { RouterLink: { template: '<a href="#"><slot /></a>' } };
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });
it("keeps strip and title heights stable, frames brand marks, and updates scrolling hints at both boundaries", async () => {
  const style = document.createElement("style");
  style.textContent = readFileSync("src/style.css", "utf8");
  document.head.append(style);
  const host = document.createElement("div"); host.className = "admin-layout"; document.body.append(host);
  const vendor = { ...managedVendors[0]!, icon: "/wide-logo.svg", apps: [{ ...adminApplications[0]!, enabled: false }], app_total: 1 };
  const wrapper = mount(VendorCard, { attachTo: host, props: { vendor, query: "", state: "current" }, global: { stubs: links } });
  const css = (selector: string) => getComputedStyle(wrapper.get(selector).element);
  try {
    const nameHeight = css(".application-preview-name").height, headingHeight = css(".directory-heading").height, rowHeight = css(".vendor-previews").gridTemplateRows;
    expect(css(".vendor-preview-scroll").overflowX).toBe("auto");
    expect(css(".vendor-preview-scroll").height).not.toBe("");
    expect(css(".vendor-previews").gridAutoFlow).toBe("column");
    expect(css(".vendor-previews").gridAutoColumns).toBe("minmax(96px, calc((100% - 2.4rem) / 4.5))");
    expect(css(".entity-icon--vendor").maxWidth).toBe("min(128px, 35%)");
    expect(css(".entity-icon--vendor img").objectFit).toBe("contain");
    expect(css(".entity-icon--vendor").borderTopStyle).toBe("solid");
    expect(css(".vendor-previews .entity-icon").borderTopStyle).toBe("solid");
    expect(css(".vendor-previews .is-disabled").backgroundColor).toBe("#eef0f2");
    expect(css(".vendor-previews .is-disabled .entity-icon").filter).toContain("grayscale(1)");
    expect(css(".add-application svg").borderTopStyle).not.toBe("solid");
    expect(wrapper.find(".strip-arrow").exists()).toBe(false);
    const viewport = wrapper.get(".vendor-preview-scroll").element as HTMLElement;
    Object.defineProperties(viewport, { clientWidth: { configurable: true, value: 400 }, scrollWidth: { configurable: true, value: 600 } });
    const scroll = vi.spyOn(viewport, "scrollBy").mockImplementation((options: ScrollToOptions | number) => {
      viewport.scrollLeft = Math.max(0, Math.min(200, viewport.scrollLeft + (typeof options === 'number' ? options : options.left || 0)));
      viewport.dispatchEvent(new Event("scroll"));
    });
    window.dispatchEvent(new Event("resize")); await flushPromises();
    expect(wrapper.get(".vendor-strip").classes()).toContain("strip-can-right");
    expect(wrapper.get(".vendor-strip").classes()).not.toContain("strip-can-left");
    expect(wrapper.get(".strip-previous").attributes("disabled")).toBeDefined();
    await wrapper.get(".strip-next").trigger("click"); await flushPromises();
    expect(scroll).toHaveBeenCalled();
    expect(wrapper.get(".strip-next").attributes("disabled")).toBeDefined();
    expect(wrapper.get(".vendor-strip").classes()).toContain("strip-can-left");
    expect(wrapper.get(".vendor-strip").classes()).not.toContain("strip-can-right");
    await wrapper.get(".strip-previous").trigger("click"); await flushPromises();
    expect(wrapper.get(".strip-previous").attributes("disabled")).toBeDefined();
    Object.defineProperty(viewport, "scrollWidth", { value: 400 });
    window.dispatchEvent(new Event("resize")); await flushPromises();
    expect(wrapper.find(".strip-arrow").exists()).toBe(false);
    expect(wrapper.get(".vendor-strip").classes()).not.toContain("strip-can-right");
    expect(wrapper.get(".vendor-strip").classes()).not.toContain("strip-can-left");
    await wrapper.setProps({ vendor: { ...vendor, name: { en: "An extremely long vendor name ".repeat(12), "zh-CN": "很长的厂商名称" }, description: { en: "Long description ".repeat(25), "zh-CN": "" }, apps: [{ ...vendor.apps[0]!, name: { en: "Long application title ".repeat(20), "zh-CN": "很长的应用名称" } }] } });
    expect(css(".application-preview-name").height).toBe(nameHeight);
    expect(css(".directory-heading").height).toBe(headingHeight);
    expect(css(".vendor-previews").gridTemplateRows).toBe(rowHeight);
    await wrapper.setProps({ vendor: { ...vendor, apps: [], app_total: 0 } });
    expect(css(".directory-heading").height).toBe(headingHeight);
    expect(css(".vendor-previews").gridTemplateRows).toBe(rowHeight);
    expect(wrapper.findAll(".vendor-previews li")).toHaveLength(1);
    expect(wrapper.get(".vendor-previews li").classes()).toContain("add-application");
  } finally { wrapper.unmount(); host.remove(); style.remove(); }
});

it("collects every API page, keeps add last, and retries a partial-load failure without a paging UI", async () => {
  const apps = Array.from({ length: 201 }, (_, i) => ({ ...adminApplications[0]!, uid: `strip-${i}`, id: `tool-${i}`, key: `openai/tool-${i}`, name: { en: `Tool ${i}`, "zh-CN": `工具${i}` } }));
  let fail = true;
  const fetch = vi.fn(async (url: string) => {
    const params = new URL(url, "https://test").searchParams, page = Number(params.get('page'));
    expect(params.get('limit')).toBe('100'); expect(params.get('q')).toBe('Tool'); expect(params.get('state')).toBe('current');
    if (page === 2 && fail) return response({ error: { code: "INTERNAL_ERROR" } }, 500);
    return response({ items: apps.slice((page - 1) * 100, page * 100), page, total: apps.length, total_pages: 3 });
  });
  vi.stubGlobal('fetch', fetch);
  const wrapper = mount(VendorCard, { props: { vendor: { ...managedVendors[0]!, apps: apps.slice(0, 5), app_total: 201 }, query: "Tool", state: "current" }, global: { stubs: links } });
  await flushPromises();
  expect(wrapper.findAll('.vendor-previews li')).toHaveLength(101);
  expect(wrapper.find('[role="alert"]').exists()).toBe(true);
  fail = false;
  await wrapper.get('[aria-label="Retry"]').trigger('click'); await flushPromises();
  expect(wrapper.findAll('.vendor-previews li')).toHaveLength(202);
  expect(wrapper.findAll('.vendor-previews li').at(-1)!.classes()).toContain('add-application');
  expect(wrapper.text()).toContain('Tool 200');
  expect(wrapper.find('[role="alert"]').exists()).toBe(false);
  expect(wrapper.find('.page-navigation').exists()).toBe(false);
  expect(fetch.mock.calls.map(([url]) => new URL(url, 'https://test').searchParams.get('page'))).toEqual(['1', '2', '1', '2', '3']);
  wrapper.unmount();
});
