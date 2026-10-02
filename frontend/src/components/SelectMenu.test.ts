import { mount } from "@vue/test-utils";
import { afterEach, expect, it } from "vitest";
import SelectMenu from "./SelectMenu.vue";
const options = [
  { value: "a", label: "Alpha" },
  { value: "b", label: "Beta" },
  { value: "c", label: "Bravo" },
  { value: "z", label: "Zulu" },
];
afterEach(() => {
  document.body.innerHTML = "";
});
it("keeps focus on the combobox and commits keyboard selection only after confirmation", async () => {
  const w = mount(SelectMenu, {
    attachTo: document.body,
    props: { modelValue: "a", options, label: "Example" },
  });
  const b = w.get("button");
  (b.element as HTMLButtonElement).focus();
  await b.trigger("keydown", { key: "ArrowDown" });
  expect(b.attributes("aria-expanded")).toBe("true");
  expect(b.attributes("aria-controls")).toBe(
    w.get("[role=listbox]").attributes("id"),
  );
  expect(document.activeElement).toBe(b.element);
  expect(w.emitted("update:modelValue")).toBeUndefined();
  await b.trigger("keydown", { key: "End" });
  expect(b.attributes("aria-activedescendant")).toBe(
    w.findAll("[role=option]")[3]!.attributes("id"),
  );
  expect(w.findAll("[aria-selected=true]")).toHaveLength(1);
  await b.trigger("keydown", { key: "Escape" });
  expect(w.find("[role=listbox]").exists()).toBe(false);
  expect(b.attributes("aria-controls")).toBeUndefined();
  expect(w.emitted("update:modelValue")).toBeUndefined();
  await b.trigger("keydown", { key: "Enter" });
  await b.trigger("keydown", { key: "ArrowDown" });
  await b.trigger("keydown", { key: "Enter" });
  expect(w.emitted("update:modelValue")).toEqual([["b"]]);
  expect(document.activeElement).toBe(b.element);
  w.unmount();
});
it("supports typeahead, Home/End, Tab and clicking an option without focus loss", async () => {
  const w = mount(SelectMenu, {
    attachTo: document.body,
    props: { modelValue: "a", options, label: "Example" },
  });
  const b = w.get("button");
  await b.trigger("keydown", { key: "b" });
  expect(w.get("[aria-selected=true]").text()).toBe("Beta");
  await b.trigger("keydown", { key: "b" });
  expect(w.get("[aria-selected=true]").text()).toBe("Bravo");
  await b.trigger("keydown", { key: "Tab" });
  expect(w.emitted("update:modelValue")?.at(-1)).toEqual(["c"]);
  await b.trigger("click");
  await b.trigger("keydown", { key: "Home" });
  await w.findAll("[role=option]")[3]!.trigger("click");
  expect(w.emitted("update:modelValue")?.at(-1)).toEqual(["z"]);
  expect(document.activeElement).toBe(b.element);
  expect(w.find("select").exists()).toBe(false);
  expect(b.attributes("aria-label")).toBe("Example");
  w.unmount();
});
it("closes on outside input, blur and disable, without trapping focus", async () => {
  const w = mount(SelectMenu, {
    attachTo: document.body,
    props: { modelValue: "a", options, label: "Example" },
  });
  const b = w.get("button");
  await b.trigger("click");
  document.body.dispatchEvent(
    new PointerEvent("pointerdown", { bubbles: true }),
  );
  await w.vm.$nextTick();
  expect(b.attributes("aria-expanded")).toBe("false");
  expect(w.emitted("update:modelValue")).toBeUndefined();
  await b.trigger("click");
  await b.trigger("keydown", { key: "ArrowDown" });
  await b.trigger("focusout", { relatedTarget: document.body });
  expect(w.emitted("update:modelValue")?.at(-1)).toEqual(["b"]);
  await b.trigger("click");
  await w.setProps({ disabled: true });
  expect(b.attributes("disabled")).toBeDefined();
  expect(w.find("[role=listbox]").exists()).toBe(false);
  w.unmount();
});
