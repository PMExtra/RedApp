import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import PublicURLSettings from "./PublicURLSettings.vue";
import { bootstrap } from "../bootstrap";
import { boot, response, resetStores } from "../testSupport";
afterEach(() => {
  vi.unstubAllGlobals();
  resetStores();
});
it("clears the override with null and revision, then immediately shows the environment origin and refreshes command state", async () => {
  bootstrap.value = boot;
  const value = {
    override_url: "https://override.example",
    environment_url: "https://env.example",
    effective_url: "https://override.example",
    source: "override",
    revision: 2,
  };
  const fetch = vi.fn(async (_url: string, init?: RequestInit) =>
    response(
      init?.method === "PUT"
        ? {
            ...value,
            override_url: null,
            effective_url: "https://env.example",
            source: "environment",
            revision: 3,
          }
        : value,
    ),
  );
  vi.stubGlobal("fetch", fetch);
  const w = mount(PublicURLSettings);
  await flushPromises();
  expect(w.text()).toContain("Administrator override");
  await w
    .findAll("button")
    .find((b) => b.text() === "Clear override")!
    .trigger("click");
  await w.find("form").trigger("submit");
  await flushPromises();
  expect(JSON.parse(fetch.mock.calls.at(-1)![1]!.body as string)).toEqual({
    override_url: null,
  });
  expect(
    (fetch.mock.calls.at(-1)![1]!.headers as Record<string, string>)[
      "If-Match"
    ],
  ).toBe('"2"');
  expect(w.text()).toContain("Environment");
  expect(bootstrap.value?.public_origin).toBe("https://env.example");
  w.unmount();
});
