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
  const localizedApps = boot.apps.map(app => ({ ...app, vendor: { id: app.id.split("/")[0]!, name: { en: "English vendor", "zh-CN": "中文厂商" } } }));
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      response(
        url === "/api/home"
          ? { pinned: localizedApps, ranking: [] }
          : {
              ...boot,
              apps: localizedApps.map((app) => ({
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
  const card = wrapper.get(".application-card-brand");
  expect(card.get(".application-card-heading h2").text()).toBe("Codex CLI");
  expect(card.get(".application-card-heading .app-publisher").text()).toBe("English vendor");
  expect(card.get(".application-card-icon").element.firstElementChild?.tagName.toLowerCase()).toMatch(/^(img|svg)$/);
  setLanguage("zh-CN");
  await flushPromises();
  expect(wrapper.get(".app-publisher").text()).toBe("中文厂商");
  setLanguage("en");
  await wrapper.find(".application-card").trigger("click");
  await flushPromises();
  await flushPromises();
  await vi.waitFor(() => expect(router.currentRoute.value.path).toBe("/openai/codex"));
  await vi.waitFor(() => expect(wrapper.find("iframe").exists()).toBe(true));
  expect(wrapper.find(".application-identity .eyebrow").exists()).toBe(false);
  expect(wrapper.text()).not.toContain("Installation instructions");
  expect(wrapper.get("iframe").attributes("src")).toBe(
    "/api/apps/openai/codex/instructions/document?lang=en",
  );
  expect(
    wrapper.findAll(".breadcrumbs a").map((link) => link.attributes("href")),
  ).toEqual(["/all", "/openai"]);
  setLanguage("zh-CN");
  await flushPromises();
  expect(wrapper.get("iframe").attributes("src")).toContain("lang=zh-CN");
  expect(wrapper.findAll(".breadcrumbs a")[1]!.text()).toBe("中文厂商");
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

it("shows popular applications without counts and puts version metadata beside the detail title", async () => {
  const apps = boot.apps.map(app => ({ ...app, download_clients: 123456, latest_known_version: { version: "1.2.3", first_seen: "2026-10-06T00:30:00Z" }, instructions: { en: "# Custom document heading", "zh-CN": "# 自定义标题" } }));
  vi.stubGlobal("fetch", vi.fn(async (url: string) => response(url === "/api/home" ? { pinned: [], ranking: apps } : { ...boot, apps })));
  const { wrapper, router } = await mountPage("/");
  expect(wrapper.text()).toContain("Popular applications");
  expect(wrapper.text()).not.toContain("123456");
  expect(wrapper.text()).not.toContain("Approximate unique");
  expect(wrapper.findAll(".application-card-meta .card-version")).toHaveLength(2);
  expect(wrapper.find(".application-card-footer").exists()).toBe(false);
  const link = wrapper.get(".application-card");
  (link.element as HTMLElement).focus();
  expect(document.activeElement).toBe(link.element);
  expect(link.get('[role="tooltip"]').text()).not.toContain(":");
  expect(link.attributes("aria-describedby")).toBe(link.get('[role="tooltip"]').attributes("id"));
  setLanguage("zh-CN"); await flushPromises();
  expect(wrapper.text()).toContain("热门应用");
  await router.push("/openai/codex"); await flushPromises();
  expect(wrapper.findAll(".application-version")).toHaveLength(1);
  expect(wrapper.get(".application-version").element.parentElement?.classList.contains("public-app-identity")).toBe(true);
  expect(wrapper.find(".public-heading .application-version").exists()).toBe(false);
  expect(wrapper.find(".usage-instructions h2").exists()).toBe(false);
  expect(wrapper.find(".application-identity .eyebrow").exists()).toBe(false);
  expect(wrapper.get(".application-version time").attributes("title")).toContain(":");
  wrapper.unmount();
});
