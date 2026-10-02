import { mount, flushPromises } from "@vue/test-utils";
import { it, expect } from "vitest";
import AccountMenu from "./AccountMenu.vue";
import SelectMenu from "./SelectMenu.vue";
import { defineComponent } from "vue";
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

it("shares dismissal without merging account and selection semantics", async () => {
  const page = defineComponent({
    components: { AccountMenu, SelectMenu },
    template: `<AccountMenu/><SelectMenu model-value="en" :options="[{value:'en',label:'English'}]" label="Language"/>`,
  });
  const wrapper = mount(page, { attachTo: document.body });
  await wrapper.find("[role=combobox]").trigger("click");
  expect(wrapper.find("[role=listbox]").exists()).toBe(true);
  await wrapper.find(".account-trigger").trigger("click");
  await flushPromises();
  expect(wrapper.find("[role=listbox]").exists()).toBe(false);
  expect(wrapper.find("[role=menu]").exists()).toBe(true);
  expect(document.activeElement?.getAttribute("role")).toBe("menuitem");
  await wrapper.find("[role=combobox]").trigger("click");
  await flushPromises();
  expect(wrapper.find("[role=menu]").exists()).toBe(false);
  expect(wrapper.find("[role=listbox]").exists()).toBe(true);
  wrapper.unmount();
});
