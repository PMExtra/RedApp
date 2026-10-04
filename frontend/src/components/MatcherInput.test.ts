import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import MatcherInput from "./MatcherInput.vue";
import { response } from "../testSupport";

afterEach(() => {
  vi.unstubAllGlobals();
});
it("uses the server's full-path result for RE2 and discards a delayed result after the matcher scope changes", async () => {
  let delayed = false,
    resolveOld: ((value: unknown) => void) | undefined;
  const fetch = vi.fn((url: string, init?: RequestInit) =>
    delayed
      ? new Promise((resolve) => {
          resolveOld = resolve;
        })
      : Promise.resolve(
          response({ matches: false, canonical_path: "/releases/tool.zip" }),
        ),
  );
  vi.stubGlobal("fetch", fetch);
  const wrapper = mount(MatcherInput, {
    props: {
      application: "acme/files",
      modelValue: { type: "re2", pattern: "releases" },
    },
  });
  await wrapper.get('[name="sample_path"]').setValue("/releases/tool.zip");
  await wrapper.get(".matcher-test button").trigger("click");
  await flushPromises();
  expect(fetch.mock.calls[0]![0]).toBe(
    "/admin/api/apps/acme/files/cache/match",
  );
  expect(JSON.parse(fetch.mock.calls[0]![1]!.body as string)).toEqual({
    match: { type: "re2", pattern: "releases" },
    path: "/releases/tool.zip",
  });
  expect(wrapper.get(".matcher-result").text()).toContain("Does not match");
  expect(wrapper.text()).toContain("complete path, not a substring");
  delayed = true;
  await wrapper.get('[name="sample_path"]').setValue("/releases/文件.zip");
  expect(wrapper.find(".matcher-result").exists()).toBe(false);
  await wrapper.get(".matcher-test button").trigger("click");
  const request = fetch.mock.calls.at(-1)![1]!;
  await wrapper.setProps({
    application: "acme/other",
    modelValue: { type: "glob", pattern: "/releases/*/" },
  });
  expect(request.signal?.aborted).toBe(true);
  resolveOld?.(
    response({ matches: true, canonical_path: "/releases/文件.zip" }),
  );
  await flushPromises();
  expect(wrapper.find(".matcher-result").exists()).toBe(false);
  expect(wrapper.text()).toContain("immediate child-directory subtrees");
  wrapper.unmount();
});
