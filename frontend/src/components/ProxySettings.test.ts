import { mount, flushPromises } from "@vue/test-utils";
import { it, expect, vi } from "vitest";
import {selectValue} from "../testSupport";
import ProxySettings from "./ProxySettings.vue";
it("edits the redacted saved URL and clears it without credential actions", async () => {
  let url: string | undefined = "http://user:****@proxy.example:3128",
    revision = 4;
  const fetch = vi.fn(async (_url: string, options?: RequestInit) => {
    if (options?.body) {
      expect(options.headers).toMatchObject({
        "If-Match": '\"' + revision + '\"',
      });
      const proxy = JSON.parse(options.body as string);
      url = proxy.mode === "direct" ? undefined : proxy.url.replace(/:[^:@/]*@/, ":****@");
      revision++;
    }
    return {
      ok: true,
      status: 200,
      json: async () => ({ mode: url ? "url" : "direct", url, revision, dns: url ? "proxy" : "local" }),
    };
  });
  vi.stubGlobal("fetch", fetch);
  const wrapper = mount(ProxySettings);
  await flushPromises();
  expect((wrapper.get("input").element as HTMLInputElement).value).toBe(url);
  expect(wrapper.findAll("input")).toHaveLength(1);
  expect(wrapper.find(".redacted-password-hint").exists()).toBe(true);
  // An unchanged redacted URL is not resubmitted.
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(fetch).toHaveBeenCalledTimes(1);
  await wrapper
    .get("input")
    .setValue("socks5://next:secret@proxy.example:1080");
  expect(wrapper.find(".redacted-password-hint").exists()).toBe(false);
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(JSON.parse(fetch.mock.calls.at(-1)![1]!.body as string)).toEqual({
    mode:"url",url: "socks5://next:secret@proxy.example:1080",
  });
  expect((wrapper.get("input").element as HTMLInputElement).value).toBe("socks5://next:****@proxy.example:1080");
  await selectValue(wrapper.get(".proxy-section"),"direct");
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(url).toBeUndefined();
  wrapper.unmount();
  vi.unstubAllGlobals();
});
