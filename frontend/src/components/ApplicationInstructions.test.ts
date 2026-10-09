import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import ApplicationInstructions from "./ApplicationInstructions.vue";
import {
  configurationFixture,
  resetStores,
  response,
  boot,
} from "../testSupport";
import { signedIn } from "../session";

beforeEach(() => {
  resetStores();
  signedIn.value = true;
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  signedIn.value = false;
});
it("does not write an unchanged document and retains equal-value custom and per-language unset operations", async () => {
  let config = configurationFixture(
    { instructions: { en: "Same text", "zh-CN": "中文" } },
    7,
    "openai/codex",
  );
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === "PATCH") {
      const body = JSON.parse(init.body as string);
      config = { ...config, revision: 8, instructions_revision: 4 };
      if (body.set["instructions.en"])
        config.fields["instructions.en"] = {
          source: "custom",
          differs_from_template: false,
        };
      return response(config);
    }
    return response(url.endsWith("/configuration") ? config : boot);
  });
  vi.stubGlobal("fetch", fetch);
  const wrapper = mount(ApplicationInstructions, {
    props: { application: "openai/codex" },
  });
  await flushPromises();
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(
    fetch.mock.calls.filter(([, init]) => init?.method === "PATCH"),
  ).toHaveLength(0);
  expect(wrapper.find(".field-reset").exists()).toBe(false);
  // Typing the inherited text again is still an explicit custom value.
  await wrapper.get("[name=instructions-en]").setValue("Same text");
  expect(wrapper.findAll(".field-reset")).toHaveLength(1);
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  const first = JSON.parse(
    fetch.mock.calls.find(([, init]) => init?.method === "PATCH")![1]!
      .body as string,
  );
  expect(first).toEqual({
    revision: 7,
    set: { "instructions.en": "Same text" },
    unset: [],
  });
  expect(wrapper.findAll(".field-reset")).toHaveLength(1);
  await wrapper.get(".field-reset").trigger("click");
  expect(wrapper.find(".field-reset").exists()).toBe(false);
  await wrapper.get("[name=instructions-zh-CN]").setValue("新说明");
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  const last = JSON.parse(
    fetch.mock.calls.filter(([, init]) => init?.method === "PATCH").at(-1)![1]!
      .body as string,
  );
  expect(last).toEqual({
    revision: 8,
    set: { "instructions.zh-CN": "新说明" },
    unset: ["instructions.en"],
  });
  wrapper.unmount();
});
it("keeps a failed bilingual draft and its revision until reload, and surfaces missing templates", async () => {
  const config = configurationFixture(
    { instructions: { en: "Old", "zh-CN": "旧" } },
    3,
    "openai/codex",
  );
  config.template_missing = true;
  const fetch = vi.fn(async (_url: string, init?: RequestInit) =>
    init?.method === "PATCH"
      ? response({ error: { code: "DIRECTORY_REVISION_CONFLICT" } }, 409)
      : response(config),
  );
  vi.stubGlobal("fetch", fetch);
  const wrapper = mount(ApplicationInstructions, {
    props: { application: "openai/codex" },
  });
  await flushPromises();
  expect(wrapper.text()).toContain("last accepted defaults");
  await wrapper.get("[name=instructions-en]").setValue("Keep draft");
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(
    (wrapper.get("[name=instructions-en]").element as HTMLTextAreaElement)
      .value,
  ).toBe("Keep draft");
  expect(wrapper.get("[role=alert]").text()).toContain("draft is preserved");
  wrapper.unmount();
});
