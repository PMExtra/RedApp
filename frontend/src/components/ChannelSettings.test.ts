import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import ChannelSettings from "./ChannelSettings.vue";
import { configurationFixture, response, resetStores } from "../testSupport";
afterEach(() => vi.unstubAllGlobals());
it("does not write an unchanged TTL and submits only the TTL leaf with template restoration", async () => {
  resetStores();
  let config = configurationFixture(
    { cache_ttl_seconds: 60 },
    3,
    "openai/codex",
  );
  const fetch = vi.fn(async (_url: string, init?: RequestInit) => {
    if (init?.method === "PATCH") {
      config = { ...config, revision: 4 };
      config.fields.cache_ttl_seconds = {
        source: "custom",
        differs_from_template: true,
      };
      (config.effective as Record<string, unknown>).cache_ttl_seconds = 120;
    }
    return response(config);
  });
  vi.stubGlobal("fetch", fetch);
  const wrapper = mount(ChannelSettings, {
    props: { application: "openai/codex" },
  });
  await flushPromises();
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(
    fetch.mock.calls.filter(([, init]) => init?.method === "PATCH"),
  ).toHaveLength(0);
  await wrapper.get("input").setValue("120");
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(
    JSON.parse(
      fetch.mock.calls.filter(([, init]) => init?.method === "PATCH")[0]![1]!
        .body as string,
    ),
  ).toEqual({ revision: 3, set: { cache_ttl_seconds: 120 }, unset: [] });
  await wrapper.get(".field-reset").trigger("click");
  expect(wrapper.find(".field-reset").exists()).toBe(false);
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(
    JSON.parse(
      fetch.mock.calls
        .filter(([, init]) => init?.method === "PATCH")
        .at(-1)![1]!.body as string,
    ),
  ).toEqual({ revision: 4, set: {}, unset: ["cache_ttl_seconds"] });
  wrapper.unmount();
});
