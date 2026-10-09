import { readFileSync } from "node:fs";
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import VendorCard from "./VendorCard.vue";
import { setLanguage } from "../i18n";
import { adminApplications, managedVendors, response } from "../testSupport";

const links = { RouterLink: { template: '<a href="#"><slot /></a>' } };
afterEach(() => { setLanguage("en"); vi.unstubAllGlobals(); vi.restoreAllMocks(); });
it("keeps strip and title heights stable, watermarks vendor marks with frameless preview icons, and updates scrolling hints at both boundaries", async () => {
  const style = document.createElement("style");
  style.textContent = readFileSync("src/style.css", "utf8");
  document.head.append(style);
  const host = document.createElement("div"); host.className = "admin-layout"; document.body.append(host);
  const vendor = { ...managedVendors[0]!, icon: "/wide-logo.svg", apps: [{ ...adminApplications[0]!, enabled: false }], app_total: 1 };
  const wrapper = mount(VendorCard, { attachTo: host, props: { vendor, query: "", state: "current" }, global: { stubs: links } });
  const css = (selector: string) => getComputedStyle(wrapper.get(selector).element);
  try {
    const headingHeight = css(".directory-heading").height, rowHeight = css(".vendor-previews li").height;
    expect(css(".vendor-preview-scroll").overflowX).toBe("auto");
    expect(css(".vendor-preview-scroll").overflowY).toBe("hidden");
    expect(parseFloat(css(".vendor-strip").paddingInline)).toBe(0);
    expect(wrapper.find('.vendor-card-description').exists()).toBe(false);
    expect(wrapper.find('.vendor-card-state').exists()).toBe(false);
    expect(css('.add-application-label').opacity).not.toBe('0');
    expect(css(".vendor-preview-scroll").height).not.toBe("");
    expect(css(".vendor-previews").flexWrap).toBe("nowrap");
    expect(css(".vendor-previews li").width).toBe("100px");
    expect(css(".entity-icon--vendor").maxWidth).toBe("35%");
    expect(css(".entity-icon--vendor").height).toBe("60px");
    expect(css(".vendor-card-heading h2 a").height).not.toBe("2.6em");
    expect(css(".app-count").marginTop).toBe("2px");
    expect(css(".directory-heading").alignItems).toBe("center");
    expect(Number(css(".entity-icon--vendor").opacity)).toBe(.22);
    expect(css(".entity-icon--vendor").pointerEvents).toBe("none");
    expect(css(".entity-icon--vendor img").objectFit).toBe("contain");
    expect(css(".entity-icon--vendor").borderTopStyle).not.toBe("solid");
    expect(css(".vendor-previews .entity-icon").borderTopStyle).not.toBe("solid");
    expect(css(".vendor-previews .is-disabled a").color).toBe("#627580");
    expect(css(".vendor-previews .is-disabled .entity-icon").filter).toContain("grayscale(1)");
    expect(css(".add-application svg").borderTopStyle).not.toBe("solid");
    expect(wrapper.find(".strip-arrow").exists()).toBe(false);
    const viewport = wrapper.get(".vendor-preview-scroll").element as HTMLElement;
    Object.defineProperties(viewport, { clientWidth: { configurable: true, value: 400 }, scrollWidth: { configurable: true, value: 600 } });
    const scroll = vi.spyOn(viewport, "scrollBy").mockImplementation((options: ScrollToOptions | number) => {
      viewport.scrollLeft = Math.max(0, Math.min(200, viewport.scrollLeft + (typeof options === 'number' ? options : options.left || 0)));
      viewport.dispatchEvent(new Event("scroll"));
    });
    const scrollTo = vi.spyOn(viewport, "scrollTo").mockImplementation((options: ScrollToOptions | number) => {
      viewport.scrollLeft = typeof options === 'number' ? options : options.left || 0;
      viewport.dispatchEvent(new Event("scroll"));
    });
    window.dispatchEvent(new Event("resize")); await flushPromises();
    expect(wrapper.get(".vendor-strip").classes()).toContain("strip-can-right");
    expect(wrapper.get(".vendor-strip").classes()).not.toContain("strip-can-left");
    expect(wrapper.get(".strip-previous").attributes("disabled")).toBeDefined();
    expect(css('.strip-next').visibility).toBe('hidden');
    expect(css('.strip-next').pointerEvents).toBe('none');
    await wrapper.trigger('mouseenter');
    expect(css('.strip-next').visibility).toBe('visible');
    await wrapper.get('.vendor-preview-scroll').trigger('focusin');
    await wrapper.trigger('mouseleave');
    expect(css('.strip-next').visibility).toBe('hidden');
    expect(wrapper.get('.strip-next').attributes('tabindex')).toBe('-1');
    expect(css('.vendor-previews li').flexShrink).toBe('0');
    expect(css('.add-application a').height).toBe('100%');
    expect(wrapper.get('.add-application').text()).toBe('Add App');
    expect(wrapper.find('.vendor-previews .state-label').exists()).toBe(false);
    expect(wrapper.get('.vendor-previews .entity-icon').attributes('style')).toContain('52px');
    viewport.scrollLeft=100; await wrapper.get('.vendor-preview-scroll').trigger('scroll');
    expect(wrapper.get('.vendor-strip').classes()).toContain('strip-can-left');
    expect(wrapper.get('.vendor-strip').classes()).toContain('strip-can-right');
    vi.useFakeTimers();
    await wrapper.get('.vendor-preview-scroll').trigger('scroll');
    expect(wrapper.get('.vendor-preview-scroll').classes()).toContain('is-scrolling');
    await vi.advanceTimersByTimeAsync(701);
    expect(wrapper.get('.vendor-preview-scroll').classes()).not.toContain('is-scrolling');
    vi.useRealTimers();
    viewport.scrollLeft=0; await wrapper.get('.vendor-preview-scroll').trigger('scroll');

    await wrapper.get(".vendor-preview-scroll").trigger("keydown", { key: "ArrowRight" }); await flushPromises();
    expect(scroll).toHaveBeenCalled();
    expect(wrapper.get(".strip-next").attributes("disabled")).toBeDefined();
    expect(wrapper.get(".vendor-strip").classes()).toContain("strip-can-left");
    expect(wrapper.get(".vendor-strip").classes()).not.toContain("strip-can-right");
    await wrapper.get(".vendor-preview-scroll").trigger("keydown", { key: "End" }); await flushPromises();
    expect(scrollTo).toHaveBeenLastCalledWith({ left: 200 });
    expect(wrapper.get(".vendor-strip").classes()).not.toContain("strip-can-right");
    await wrapper.get(".vendor-preview-scroll").trigger("keydown", { key: "Home" }); await flushPromises();
    expect(scrollTo).toHaveBeenLastCalledWith({ left: 0 });
    await wrapper.get(".strip-previous").trigger("click"); await flushPromises();
    expect(wrapper.get(".strip-previous").attributes("disabled")).toBeDefined();
    Object.defineProperty(viewport, "scrollWidth", { value: 400 });
    window.dispatchEvent(new Event("resize")); await flushPromises();
    expect(wrapper.find(".strip-arrow").exists()).toBe(false);
    expect(wrapper.get(".vendor-strip").classes()).not.toContain("strip-can-right");
    expect(wrapper.get(".vendor-strip").classes()).not.toContain("strip-can-left");
    await wrapper.setProps({ vendor: { ...vendor, name: { en: "An extremely long vendor name ".repeat(12), "zh-CN": "很长的厂商名称" }, description: { en: "Long description ".repeat(25), "zh-CN": "" }, apps: [{ ...vendor.apps[0]!, name: { en: "Long application title ".repeat(20), "zh-CN": "很长的应用名称" } }] } });
    expect(css(".application-preview-name").maxHeight).toBe("36px");
    expect(css(".directory-heading").height).toBe(headingHeight);
    expect(css(".vendor-previews li").height).toBe(rowHeight);
    await wrapper.setProps({ vendor: { ...vendor, apps: [], app_total: 0 } });
    expect(css(".directory-heading").height).toBe(headingHeight);
    expect(css(".vendor-previews li").height).toBe(rowHeight);
    expect(wrapper.findAll(".vendor-previews li")).toHaveLength(1);
    expect(wrapper.get(".vendor-previews li").classes()).toContain("add-application");
  } finally { vi.useRealTimers(); wrapper.unmount(); host.remove(); style.remove(); }
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


it("keeps a count-independent strip, aligned title rows and always-visible add control across widths and languages", async () => {
  const style = document.createElement("style");
  style.textContent = readFileSync("src/style.css", "utf8"); document.head.append(style);
  const host = document.createElement("div"); host.className = "admin-layout"; document.body.append(host);
  try {
    for (const width of [280, 640]) for (const lang of ["en", "zh-CN"] as const) for (const count of [0, 1, 4, 6, 20]) {
      setLanguage(lang); host.style.width = `${width}px`;
      const names = [{en:"App", "zh-CN":"应用"}, {en:"Two line application", "zh-CN":"两行应用名称示例"}, {en:"Long title ".repeat(20), "zh-CN":"很长的应用名称".repeat(20)}];
      const apps = Array.from({length:count}, (_,i)=>({...adminApplications[0]!, uid:`tile-${i}`, name:names[i%3]!, enabled:i!==0}));
      const wrapper = mount(VendorCard, {attachTo:host, props:{vendor:{...managedVendors[0]!,apps,app_total:count}, query:"",state:"current"},global:{stubs:links}});
      try {
        const css=(selector:string)=>getComputedStyle(wrapper.get(selector).element);
        expect(wrapper.findAll('.vendor-previews > li')).toHaveLength(count+1);
        expect(wrapper.get('.vendor-previews').attributes('style')).toBeUndefined();
        for(const tile of wrapper.findAll('.vendor-previews > li')) {
          const computed=getComputedStyle(tile.element);
          expect(computed.width).toBe('100px');expect(computed.minWidth).toBe('100px');expect(computed.flexBasis).toBe('100px');expect(computed.flexShrink).toBe('0');expect(computed.height).toBe('124px');
          const control=getComputedStyle(tile.get('a').element);
          expect(control.boxSizing).toBe('border-box');
          for(const side of ['Top','Right','Bottom','Left']) expect(parseFloat(control.getPropertyValue(`border-${side.toLowerCase()}-width`)) || 0).toBe(tile.classes().includes('add-application')?1:0);
          expect(control.borderTopStyle).not.toBe('solid');
          if(tile.classes().includes('add-application')) expect(control.borderTopStyle).toBe('dashed');
        }
        expect(css('.vendor-previews').width).toBe('max-content');
        expect(css('.vendor-previews').flexWrap).toBe('nowrap');
        expect(css('.vendor-preview-scroll').overflowY).toBe('hidden');
        expect(css('.vendor-previews > li > a').justifyContent).toBe('flex-start');
        expect(css('.add-application-label').position).not.toBe('absolute');
        const add=css('.add-application a');
        const addTop=parseFloat(add.paddingTop)+parseFloat(add.borderTopWidth);
        expect(addTop).toBe(18);
        expect(addTop+44+parseFloat(add.gap)).toBe(74);
        expect(wrapper.get('.add-application-label').text()).toBe(lang==='en'?'Add App':'添加应用');
        expect(css('.add-application-label').opacity).not.toBe('0');
        expect(wrapper.get('.add-application a').attributes('aria-label')).toBe(lang==='en'?'Add application':'添加应用');
        expect(wrapper.findAll('.vendor-previews > li').at(-1)!.classes()).toContain('add-application');
        expect(wrapper.find('.vendor-strip-status').exists()).toBe(false);
        if(count) {
          const link=css('.vendor-previews > li:not(.add-application) > a');
          expect(parseFloat(link.paddingTop)).toBe(14);
          expect(parseFloat(link.paddingTop)+52+parseFloat(link.gap)).toBe(74);
          expect(css('.application-preview-name').maxHeight).toBe('36px');
          expect(css('.application-preview-name').height).not.toBe('36px');
          expect(css('.entity-icon').padding).toBe('2px');expect(css('.entity-icon').borderTopStyle).not.toBe('solid');
          expect(css('.entity-icon img').objectFit).toBe('contain');
          host.style.setProperty('--app-icon-background','#123456');
          expect(css('.entity-icon').backgroundColor).toBe('#123456');host.style.removeProperty('--app-icon-background');
          expect(wrapper.get('.is-disabled a').attributes('href')).toBe('#');
          expect(wrapper.get('.is-disabled a').attributes('aria-disabled')).toBeUndefined();
        }
      } finally {wrapper.unmount();}
    }
    // Happy DOM does not lay out pixels or emulate touch: verify interaction
    // declarations separately from the synthetic boundary tests above.
    const rules=Array.from(style.sheet!.cssRules);
    const feedback=rules.find(rule=>rule.cssText.includes('.vendor-previews > li > a:hover')) as CSSStyleRule;
    expect(feedback.selectorText).toContain(':focus-visible');
    expect(feedback.style.background).toBe('');
    expect(feedback.style.boxShadow).toBe('var(--app-tile-shadow)');
    expect(feedback.style.transform).toBe('');
    expect(rules.some(rule=>rule.cssText.includes('prefers-reduced-motion') && rule.cssText.includes('transition: none'))).toBe(true);
    // Light cards show app icons without a separate backdrop; only dark schemes add one.
    const root=rules.find(rule=>rule instanceof CSSStyleRule&&rule.selectorText===':root'&&rule.style.getPropertyValue('--app-icon-background')) as CSSStyleRule;
    expect(root.style.getPropertyValue('--app-icon-background').trim()).toBe('transparent');
    expect(rules.some(rule=>rule.cssText.includes('prefers-color-scheme: dark') && rule.cssText.includes('--app-icon-background: #f8faf9'))).toBe(true);
    expect(rules.some(rule=>rule.cssText.includes('pointer: coarse') && rule.cssText.includes('display: none'))).toBe(true);
  } finally {host.remove();style.remove();}
});
