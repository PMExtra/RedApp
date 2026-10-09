import { mount, flushPromises } from "@vue/test-utils";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
import ApplicationTaxonomy from "./ApplicationTaxonomy.vue";
import { configurationFixture, resetStores, response } from "../testSupport";
import { signedIn } from "../session";
beforeEach(() => {
  resetStores();
  signedIn.value = true;
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  document.body.innerHTML = "";
});
const categoryItems = [
  { id: "tools", name: { en: "Tools", "zh-CN": "工具" } },
  { id: "network", name: { en: "Network", "zh-CN": "网络" } },
  { id: "shared-a", name: { en: "Shared", "zh-CN": "甲" } },
  { id: "shared-b", name: { en: "乙", "zh-CN": "shared" } },
];
function harness(initial: { categories: string[]; tags: string[] }, conflict = { value: false }) {
  let cfg = configurationFixture(initial, 2, "openai/codex");
  cfg.fields.tags = { source: "custom", differs_from_template: true };
  const patches: Record<string, unknown>[] = [];
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === "PATCH") {
      const body = JSON.parse(init.body as string);
      patches.push(body);
      if (conflict.value) return response({ error: { code: "DIRECTORY_REVISION_CONFLICT" } }, 409);
      cfg = { ...cfg, revision: cfg.revision + 1, effective: { ...cfg.effective, ...body.set } };
      return response(cfg);
    }
    if (url.includes("/admin/api/categories")) return response({ items: categoryItems, total_pages: 1 });
    return response(cfg);
  });
  vi.stubGlobal("fetch", fetch);
  return { fetch, patches };
}
async function mountPanel(application = "openai/codex") {
  const wrapper = mount(ApplicationTaxonomy, { props: { application }, attachTo: document.body });
  await flushPromises();
  return wrapper;
}

it("selects existing categories, reuses typed names and keeps new ones in the draft until save", async () => {
  const { patches } = harness({ categories: ["tools"], tags: [] });
  const wrapper = await mountPanel();
  const input = wrapper.get(".category-picker input");
  await input.trigger("focus");
  await input.setValue("net");
  expect(wrapper.findAll(".category-options [role=option]").map((o) => o.text())).toEqual(["Network"]);
  await wrapper.get(".category-options [role=option]").trigger("click");
  // A typed name in either language reuses the existing category.
  await input.setValue("工具");
  await input.trigger("keydown", { key: "Enter" });
  // Composition Enter is ignored; a real Enter adds a draft-only category.
  await input.setValue("效率工具");
  await input.trigger("keydown", { key: "Enter", isComposing: true });
  expect(wrapper.findAll(".category-chips li.pending")).toHaveLength(0);
  await input.trigger("keydown", { key: "Enter" });
  await input.setValue("效率工具 ");
  await input.trigger("keydown", { key: "Enter" });
  expect(wrapper.findAll(".category-chips li.pending").map((c) => c.text())).toEqual(["效率工具New"]);
  await input.setValue("SHARED");
  await input.trigger("keydown", { key: "Enter" });
  expect(wrapper.get(".category-picker [role=alert]").text()).toContain("several categories");
  expect(patches).toHaveLength(0);
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(patches[0]).toEqual({ revision: 2, set: { categories: ["tools", "network"] }, unset: [], new_categories: ["效率工具"] });
  expect(wrapper.findAll(".category-chips li.pending")).toHaveLength(0);
  wrapper.unmount();
});

it("saves the tag being typed, rejects duplicates and handles IME, delete and cancel", async () => {
  const { patches } = harness({ categories: [], tags: ["cli"] });
  const wrapper = await mountPanel();
  expect(wrapper.get(".tag-list").text()).toContain("#cli");
  await wrapper.get('[aria-label="Add tag"]').trigger("click");
  const editor = () => wrapper.get(".tag-chip.editing input");
  expect(wrapper.get(".tag-chip.editing").text()).toContain("#");
  await editor().setValue("CLI");
  await editor().trigger("keydown", { key: "Enter" });
  expect(wrapper.get(".tag-editor [role=alert]").text()).toContain("already exists");
  expect(wrapper.findAll(".tag-list .tag-chip")).toHaveLength(1);
  await wrapper.get('[aria-label="Add tag"]').trigger("click");
  await editor().setValue("中文");
  await editor().trigger("keydown", { key: "Enter", isComposing: true });
  expect(wrapper.find(".tag-chip.editing").exists()).toBe(true);
  await editor().trigger("keydown", { key: "Escape" });
  expect(wrapper.find(".tag-chip.editing").exists()).toBe(false);
  await wrapper.get('[aria-label="Remove tag: #cli"]').trigger("click");
  await wrapper.get('[aria-label="Add tag"]').trigger("click");
  await editor().setValue("#终端");
  await editor().trigger("blur");
  expect(wrapper.get(".tag-list").text()).toContain("#终端");
  await wrapper.get('[aria-label="Add tag"]').trigger("click");
  // The last tag is still focused and typed when the form is saved.
  await editor().setValue(" 命令行 ");
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(patches[0]).toEqual({ revision: 2, set: { tags: ["终端", "命令行"] }, unset: [] });
  wrapper.unmount();
});

it("resets only the edited field, preserves additions on conflict and clears them on reload or another App", async () => {
  const conflict = { value: true };
  const { patches } = harness({ categories: [], tags: ["custom"] }, conflict);
  vi.stubGlobal("confirm", vi.fn(() => true));
  const wrapper = await mountPanel();
  const resets = () => wrapper.findAll(".field-reset").map((b) => b.attributes("aria-label"));
  expect(resets()).toEqual(["Reset: Tags"]);
  const input = wrapper.get(".category-picker input");
  await input.setValue("Draft only");
  await input.trigger("keydown", { key: "Enter" });
  expect(resets()).toEqual(["Reset: Categories", "Reset: Tags"]);
  await wrapper.get('[aria-label="Add tag"]').trigger("click");
  await wrapper.get(".tag-chip.editing input").setValue("typing");
  await wrapper.get('[aria-label="Reset: Tags"]').trigger("click");
  expect(wrapper.find(".tag-chip.editing").exists()).toBe(false);
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(patches[0]).toEqual({ revision: 2, set: { categories: [] }, unset: ["tags"], new_categories: ["Draft only"] });
  expect(wrapper.get("[role=alert]").text()).toContain("draft is preserved");
  expect(wrapper.findAll(".category-chips li.pending")).toHaveLength(1);
  await wrapper.get('[aria-label="Reset: Categories"]').trigger("click");
  expect(wrapper.findAll(".category-chips li.pending")).toHaveLength(0);
  await input.setValue("Again");
  await input.trigger("keydown", { key: "Enter" });
  await wrapper.get('[aria-label="Reload"]').trigger("click");
  await flushPromises();
  expect(wrapper.findAll(".category-chips li.pending")).toHaveLength(0);
  await input.setValue("Other app");
  await input.trigger("keydown", { key: "Enter" });
  await wrapper.setProps({ application: "anthropic/claude-code" });
  await flushPromises();
  expect(wrapper.findAll(".category-chips li.pending")).toHaveLength(0);
  wrapper.unmount();
});
