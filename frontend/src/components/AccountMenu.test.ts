import { mount, flushPromises } from "@vue/test-utils";
import { it, expect } from "vitest";
import AccountMenu from "./AccountMenu.vue";
it("supports keyboard navigation, Escape focus return and outside dismissal", async () => {
  const wrapper = mount(AccountMenu, { attachTo: document.body });
  const trigger = wrapper.find(".account-trigger");
  (trigger.element as HTMLElement).focus();
  await trigger.trigger("keydown", { key: "ArrowDown" });
  await flushPromises();
  expect(trigger.attributes("aria-expanded")).toBe("true");
  const items = wrapper.findAll("[role=menuitem]");
  expect(document.activeElement).toBe(items[0].element);
  await items[0].trigger("keydown", { key: "ArrowDown" });
  expect(document.activeElement).toBe(items[1].element);
  await items[1].trigger("keydown", { key: "Home" });
  expect(document.activeElement).toBe(items[0].element);
  await items[0].trigger("keydown", { key: "Escape" });
  expect(trigger.attributes("aria-expanded")).toBe("false");
  expect(document.activeElement).toBe(trigger.element);
  await trigger.trigger("click");
  document.body.dispatchEvent(
    new PointerEvent("pointerdown", { bubbles: true }),
  );
  await flushPromises();
  expect(wrapper.find("[role=menu]").exists()).toBe(false);
  await trigger.trigger("click");
  await wrapper.find("[role=menuitem]").trigger("click");
  expect(wrapper.emitted("password")).toHaveLength(1);
  wrapper.unmount();
});
