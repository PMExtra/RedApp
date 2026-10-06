import { mount } from "@vue/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import PopoverPanel from "./PopoverPanel.vue";
afterEach(() => vi.restoreAllMocks());
it("keeps topbar menus inside the viewport and repositions after resize", async () => {
  vi.spyOn(document.documentElement, "clientWidth", "get").mockReturnValue(320);
  let left = -35;
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
    const shift = Number(this.style.transform.match(/translateX\(([-\d.]+)px\)/)?.[1] || 0);
    return { left: left + shift, width: 210 } as DOMRect;
  });
  const w = mount(PopoverPanel, { props: { topbar: true } });
  await w.vm.$nextTick();
  expect(w.classes()).toContain("topbar-menu");
  expect(w.attributes("style")).toContain("translateX(51px)");
  left = 200;
  window.dispatchEvent(new Event("resize"));
  await w.vm.$nextTick();
  expect(w.attributes("style")).toContain("translateX(-106px)");
  w.unmount();
  const form = mount(PopoverPanel);
  expect(form.classes()).not.toContain("topbar-menu");
  expect(form.attributes("style")).toBeUndefined();
  form.unmount();
});
