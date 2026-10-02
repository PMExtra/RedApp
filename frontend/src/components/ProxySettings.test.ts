import { mount, flushPromises } from "@vue/test-utils";
import { it, expect, vi } from "vitest";
import ProxySettings from "./ProxySettings.vue";
it("keeps secrets hidden and sends explicit preserve/replace/clear actions", async () => {
  const fetch = vi.fn(async (_url: string, options?: RequestInit) => ({
    ok: true,
    status: 200,
    json: async () => ({
      server: "http://proxy.example:3128",
      has_credentials: true,
      has_password: true,
      dns: "proxy",
    }),
  }));
  vi.stubGlobal("fetch", fetch);
  const wrapper = mount(ProxySettings);
  await flushPromises();
  expect(wrapper.find("input[type=password]").exists()).toBe(false);
  await wrapper.find("form").trigger("submit");
  await flushPromises();
  expect(JSON.parse(fetch.mock.calls.at(-1)![1]!.body as string)).toEqual({
    server: "http://proxy.example:3128",
    username: "",
    password: "",
    password_action: "keep",
  });
  await wrapper.find("[role=combobox]").trigger("click");
  await wrapper.findAll("[role=option]")[1]!.trigger("click");
  await wrapper.find("input[type=password]").setValue("fixture-secret");
  await wrapper.find("form").trigger("submit");
  await flushPromises();
  expect(JSON.parse(fetch.mock.calls.at(-1)![1]!.body as string).password).toBe(
    "fixture-secret",
  );
  expect(wrapper.html()).not.toContain("fixture-secret");
  await wrapper.find("[role=combobox]").trigger("click");
  await wrapper.findAll("[role=option]")[2]!.trigger("click");
  await wrapper.find("form").trigger("submit");
  await flushPromises();
  expect(
    JSON.parse(fetch.mock.calls.at(-1)![1]!.body as string).password_action,
  ).toBe("clear");
  wrapper.unmount();
  vi.unstubAllGlobals();
});
