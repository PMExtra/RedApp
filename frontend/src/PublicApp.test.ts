import { flushPromises } from "@vue/test-utils";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { setLanguage } from "./i18n";
import { boot, mountPage, resetStores, response } from "./testSupport";
beforeEach(resetStores);
afterEach(() => {
  vi.unstubAllGlobals();
  setLanguage("en");
  document.body.innerHTML = "";
});
it("uses canonical RouterLinks, navigates between applications without reload, and preserves commands across locales", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      response(
        url === "/api/home"
          ? { pinned: boot.apps, ranking: [] }
          : {
              ...boot,
              apps: boot.apps.map((app) => ({
                ...app,
                instructions: {
                  en: "{{install_commands}}",
                  "zh-CN": "{{install_commands}}",
                },
              })),
            },
      ),
    ),
  );
  const { wrapper, router } = await mountPage("/");
  expect(wrapper.findAll(".application-card")).toHaveLength(2);
  expect(wrapper.find(".application-card").attributes("href")).toBe(
    "/openai/codex",
  );
  expect(wrapper.text()).not.toContain("organization’s download service");
  await wrapper.find(".application-card").trigger("click");
  await flushPromises();
  await flushPromises();
  expect(router.currentRoute.value.path).toBe("/openai/codex");
  expect(wrapper.get("iframe").attributes("src")).toBe(
    "/api/apps/openai/codex/instructions/document?lang=en",
  );
  expect(
    wrapper.findAll(".breadcrumbs a").map((link) => link.attributes("href")),
  ).toEqual(["/all", "/openai"]);
  setLanguage("zh-CN");
  await flushPromises();
  expect(wrapper.get("iframe").attributes("src")).toContain("lang=zh-CN");
  await router.push("/anthropic/claude-code");
  await flushPromises();
  expect(wrapper.find("h1").text()).toBe("Claude Code");
  expect(wrapper.get("iframe").attributes("src")).toContain(
    "/anthropic/claude-code/instructions/document",
  );
  expect(wrapper.text()).not.toContain("CODEX_RELEASE");
  router.back();
  await flushPromises();
  await flushPromises();
  expect(wrapper.find("h1").text()).toBe("Codex CLI");
  wrapper.unmount();
});
it("handles a direct unknown application and retries failed bootstrap with localized defaults", async () => {
  let fail = true;
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => (fail ? response({}, 503) : response(boot))),
  );
  setLanguage("zh-CN");
  const { wrapper, router } = await mountPage("/unknown/application");
  expect(wrapper.text()).toContain("无法加载应用");
  expect(wrapper.find(".brand").text()).toContain("应用再分发平台");
  fail = false;
  await wrapper.find(".empty-state button").trigger("click");
  await flushPromises();
  expect(wrapper.find("h1").text()).toBe("页面不存在");
  await router.push("/openai/codex");
  await flushPromises();
  expect(wrapper.find("h1").text()).toBe("Codex CLI");
  wrapper.unmount();
});
