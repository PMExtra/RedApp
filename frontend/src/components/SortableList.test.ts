import { defineComponent, ref } from "vue";
import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import SortableList from "./SortableList.vue";
import { setLanguage } from "../i18n";

function fixture() {
  setLanguage("en");
  const items = ref(["First", "Second", "Third"]),
    disabled = ref(false);
  const wrapper = mount(
    defineComponent({
      components: { SortableList },
      setup: () => ({ items, disabled }),
      template:
        '<SortableList v-model="items" label="Order" :item-label="item => item" :disabled="disabled"><template #default="{ item }"><input :value="item" /></template></SortableList>',
    }),
    { attachTo: document.body },
  );
  const rows = () => wrapper.findAll("[data-sortable-row]");
  const handles = () => wrapper.findAll(".sort-handle");
  return { wrapper, items, disabled, rows, handles };
}
afterEach(() => {
  vi.restoreAllMocks();
  document.body.innerHTML = "";
});

it("only reorders from its handle, bounds keyboard moves, and follows the moved item with focus and an announcement", async () => {
  const { wrapper, items, handles } = fixture();
  expect(
    handles().every(
      (h) => h.text() === "" && h.attributes("title") === "Drag to reorder",
    ),
  ).toBe(true);
  await handles()[0]!.trigger("click");
  await wrapper.get("input").trigger("keydown", { key: "ArrowDown" });
  await handles()[0]!.trigger("keydown", { key: "ArrowUp" });
  expect(items.value).toEqual(["First", "Second", "Third"]);
  await handles()[0]!.trigger("keydown", { key: "End" });
  expect(items.value).toEqual(["Second", "Third", "First"]);
  expect(document.activeElement).toBe(handles()[2]!.element);
  expect(wrapper.get('[role="status"]').text()).toContain(
    "First to position 3 of 3",
  );
  await handles()[2]!.trigger("keydown", { key: "Home" });
  await handles()[0]!.trigger("keydown", { key: "ArrowDown" });
  expect(items.value).toEqual(["Second", "First", "Third"]);
  expect(document.activeElement).toBe(handles()[1]!.element);
  wrapper.unmount();
});

it("drags with mouse or touch, ignores unrelated lists, and rolls back Escape and pointer cancellation", async () => {
  for (const pointerType of ["mouse", "touch"]) {
    const { wrapper, items, handles, rows } = fixture();
    const hit = vi
      .spyOn(document, "elementFromPoint")
      .mockReturnValue(document.body);
    const down = {
      pointerId: 2,
      pointerType,
      button: 0,
      isPrimary: true,
      clientX: 30,
      clientY: 150,
    };
    const move = { pointerId: 2, pointerType, clientX: 30, clientY: 70 };
    await handles()[2]!.trigger("pointerdown", down);
    await wrapper.get("ol").trigger("pointermove", move);
    expect(items.value).toEqual(["First", "Second", "Third"]);
    hit.mockReturnValue(rows()[0]!.element);
    vi.spyOn(rows()[0]!.element, "getBoundingClientRect").mockReturnValue({
      top: 40,
      height: 80,
    } as DOMRect);
    await wrapper.get("ol").trigger("pointermove", { ...move, clientY: 90 });
    expect(items.value).toEqual(["First", "Second", "Third"]);
    await wrapper.get("ol").trigger("pointermove", move);
    expect(items.value).toEqual(["Third", "First", "Second"]);
    hit.mockReturnValue(rows()[1]!.element);
    vi.spyOn(rows()[1]!.element, "getBoundingClientRect").mockReturnValue({
      top: 0,
      height: 200,
    } as DOMRect);
    await wrapper.get("ol").trigger("pointermove", move);
    expect(items.value).toEqual(["Third", "First", "Second"]);
    hit.mockReturnValue(rows()[0]!.element);
    await wrapper.get("ol").trigger("keydown", { key: "Escape" });
    expect(items.value).toEqual(["First", "Second", "Third"]);
    expect(document.activeElement).toBe(handles()[2]!.element);
    await handles()[2]!.trigger("pointerdown", down);
    await wrapper.get("ol").trigger("pointermove", move);
    await wrapper.get("ol").trigger("pointercancel", { pointerId: 2 });
    expect(items.value).toEqual(["First", "Second", "Third"]);
    await handles()[2]!.trigger("pointerdown", down);
    await wrapper.get("ol").trigger("pointermove", move);
    await wrapper.get("ol").trigger("pointerup", { pointerId: 2 });
    expect(items.value).toEqual(["Third", "First", "Second"]);
    expect(document.activeElement).toBe(handles()[0]!.element);
    wrapper.unmount();
    hit.mockRestore();
  }
});

it("stops stale drags when the list is replaced or disabled and cannot reorder a single item", async () => {
  const { wrapper, items, disabled, handles, rows } = fixture();
  const hit = vi
    .spyOn(document, "elementFromPoint")
    .mockReturnValue(rows()[0]!.element);
  await handles()[2]!.trigger("pointerdown", {
    pointerId: 1,
    button: 0,
    isPrimary: true,
    clientX: 20,
    clientY: 150,
  });
  items.value = ["New", "List"];
  await flushPromises();
  hit.mockReturnValue(rows()[0]!.element);
  await wrapper
    .get("ol")
    .trigger("pointermove", { pointerId: 1, clientX: 20, clientY: 60 });
  await wrapper.get("ol").trigger("pointercancel", { pointerId: 1 });
  expect(items.value).toEqual(["New", "List"]);
  await handles()[1]!.trigger("pointerdown", {
    pointerId: 3,
    button: 0,
    isPrimary: true,
    clientX: 20,
    clientY: 150,
  });
  disabled.value = true;
  await flushPromises();
  await wrapper
    .get("ol")
    .trigger("pointermove", { pointerId: 3, clientX: 20, clientY: 60 });
  await handles()[0]!.trigger("keydown", { key: "End" });
  expect(items.value).toEqual(["New", "List"]);
  expect(handles().every((h) => h.attributes("disabled") !== undefined)).toBe(
    true,
  );
  disabled.value = false;
  items.value = ["Only"];
  await flushPromises();
  expect(handles()[0]!.attributes("disabled")).toBeDefined();
  wrapper.unmount();
});
