import { mount } from "@vue/test-utils";
import { expect, it, vi } from "vitest";
import FilePicker from "./FilePicker.vue";

it("opens the native chooser through a labelled button and forwards the original file event", async () => {
  const wrapper = mount(FilePicker, { props: { name: "icon", label: "选择图标", accept: "image/png" } });
  const input = wrapper.get("input");
  const click = vi.spyOn(input.element, "click");
  expect(input.attributes("hidden")).toBeDefined();
  expect(input.attributes("aria-label")).toBe("选择图标");
  expect(wrapper.get("button").attributes("aria-controls")).toBe(input.attributes("id"));
  await wrapper.get("button").trigger("click");
  expect(click).toHaveBeenCalledOnce();
  const file = new File(["original bytes"], "icon.png", { type: "image/png" });
  Object.defineProperty(input.element, "files", { value: [file] });
  await input.trigger("change");
  const event = wrapper.emitted("change")![0]![0] as Event;
  expect((event.target as HTMLInputElement).files![0]).toBe(file);
  await wrapper.setProps({ label: "替换图标", disabled: true });
  expect(wrapper.get("button").text()).toBe("替换图标");
  await wrapper.get("button").trigger("click");
  expect(click).toHaveBeenCalledOnce();
  wrapper.unmount();
});
