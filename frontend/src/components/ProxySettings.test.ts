import { mount, flushPromises } from "@vue/test-utils";
import { it, expect, vi } from "vitest";
import ProxySettings from "./ProxySettings.vue";
it("edits the complete saved URL and clears it without credential actions", async () => {
  let server = "http://user:p%40ss@proxy.example:3128",
    revision = 4;
  const fetch = vi.fn(async (_url: string, options?: RequestInit) => {
    if (options?.body) {
      expect(options.headers).toMatchObject({
        "If-Match": '\"' + revision + '\"',
      });
      server = JSON.parse(options.body as string).server;
      revision++;
    }
    return {
      ok: true,
      status: 200,
      json: async () => ({ server, revision, dns: server ? "proxy" : "local" }),
    };
  });
  vi.stubGlobal("fetch", fetch);
  const wrapper = mount(ProxySettings);
  await flushPromises();
  expect((wrapper.get("input").element as HTMLInputElement).value).toBe(server);
  expect(wrapper.findAll("input")).toHaveLength(1);
  await wrapper
    .get("input")
    .setValue("socks5://next:secret@proxy.example:1080");
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(JSON.parse(fetch.mock.calls.at(-1)![1]!.body as string)).toEqual({
    server: "socks5://next:secret@proxy.example:1080",
  });
  await wrapper.get("input").setValue("");
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(server).toBe("");
  wrapper.unmount();
  vi.unstubAllGlobals();
});
