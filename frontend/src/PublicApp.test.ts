import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, describe, it, expect, vi } from "vitest";
import { setLanguage } from "./i18n";
import PublicApp from "./PublicApp.vue";
const apps = [
  {
    id: "codex",
    name: "Codex CLI",
    summary: "Coding agent",
    origin: "https://downloads.example:8443",
    icon: "/apps/codex/icon.svg",
  },
];
const response = (data: unknown, ok = true) => ({ ok, json: async () => data });
const info = { version: "0.4.0", os: "linux", arch: "arm64" };
afterEach(() => {
  vi.unstubAllGlobals();
  window.history.replaceState({}, "", "/");
  setLanguage("en");
});
describe("Anonymous applications", () => {
  it("lists Codex and switches all page chrome to Chinese while keeping commands unchanged", async () => {
    const fetch = vi.fn(async (url: string) =>
      response(url === "/api/info" ? info : apps),
    );
    vi.stubGlobal("fetch", fetch);
    const wrapper = mount(PublicApp);
    expect(wrapper.text()).toContain("Loading applications");
    await flushPromises();
    expect(wrapper.findAll(".application-card")).toHaveLength(1);
    expect(wrapper.find(".application-card img").attributes("src")).toBe(
      "/apps/codex/icon.svg",
    );
    expect(wrapper.find(".application-card img").attributes("alt")).toBe(
      "OpenAI brand mark",
    );
    expect(wrapper.find(".application-card").attributes("href")).toBe(
      "/apps/codex",
    );
    expect(wrapper.find(".admin-link").attributes("href")).toBe("/admin/");
    expect(wrapper.find("form").exists()).toBe(false);
    expect(fetch.mock.calls.map((call) => call[0]).sort()).toEqual([
      "/api/apps",
      "/api/info",
    ]);
    await wrapper.find(".language-control button").trigger("click");
    await wrapper
      .findAll("[role=option]")
      .find((o) => o.text().includes("简体中文"))!
      .trigger("click");
    expect(wrapper.find("h1").text()).toBe("应用");
    expect(wrapper.text()).toContain("管理后台");
    expect(wrapper.find("footer").text()).not.toContain("架构");
    expect(wrapper.find("footer").text()).toContain("v0.4.0");
    expect(wrapper.find("footer").text()).not.toContain("arm64");
    expect(document.documentElement.lang).toBe("zh-CN");
    expect(localStorage.getItem("redapp-language")).toBe("zh-CN");
    wrapper.unmount();
  });
  it("opens direct details and copies exact server-origin commands in both languages", async () => {
    window.history.replaceState({}, "", "/apps/codex");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => response(url === "/api/info" ? info : apps)),
    );
    vi.stubGlobal("isSecureContext", true);
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    const wrapper = mount(PublicApp);
    await flushPromises();
    expect(wrapper.find("h1").text()).toBe("Codex CLI");
    expect(wrapper.find(".application-identity img").attributes("src")).toBe(
      "/apps/codex/icon.svg",
    );
    const commands = [
      "curl -fsSL 'https://downloads.example:8443/install.sh' | sh",
      "irm 'https://downloads.example:8443/install.ps1' | iex",
    ];
    for (const lang of ["en", "zh-CN"]) {
      await wrapper.find(".language-control button").trigger("click");
      await wrapper
        .findAll("[role=option]")
        .find((o) => o.text().includes(lang === "en" ? "English" : "简体中文"))!
        .trigger("click");
      expect(
        wrapper.findAll(".command code").map((item) => item.text()),
      ).toEqual(commands);
      for (const button of wrapper.findAll(".command button")) {
        await button.trigger("click");
        await flushPromises();
      }
    }
    expect(writeText.mock.calls.map((call) => call[0])).toEqual([
      ...commands,
      ...commands,
    ]);
    expect(wrapper.text()).toContain("命令已复制");
    expect(wrapper.find(".application-identity img").attributes("alt")).toBe(
      "OpenAI 品牌标志",
    );
    expect(wrapper.text()).toContain("否则使用 latest");
    expect(wrapper.find(".back-link").attributes("href")).toBe("/");
    expect(wrapper.findAll(".footer-notice")).toHaveLength(1);
    expect(wrapper.find(".independent-notice").exists()).toBe(false);
    expect(wrapper.find(".review-scripts").exists()).toBe(false);
    expect(wrapper.text()).not.toContain("推广");
    expect(wrapper.text()).not.toContain("Clipboard access requires");
    wrapper.unmount();
  });
  it("retries failure once and aborts pending anonymous requests on unmount", async () => {
    let fail = true;
    let aborted = false;
    const fetch = vi.fn((url: string, options?: RequestInit) =>
      url === "/api/info"
        ? Promise.resolve(response(info))
        : fail
          ? Promise.resolve(response({}, false))
          : new Promise((_resolve, reject) =>
              options?.signal?.addEventListener("abort", () => {
                aborted = true;
                reject(new DOMException("cancel", "AbortError"));
              }),
            ),
    );
    vi.stubGlobal("fetch", fetch);
    const wrapper = mount(PublicApp);
    await flushPromises();
    expect(wrapper.find("[role=alert]").exists()).toBe(true);
    fail = false;
    await wrapper.find("[role=alert] button").trigger("click");
    expect(wrapper.text()).toContain("Loading applications");
    wrapper.unmount();
    await flushPromises();
    expect(aborted).toBe(true);
  });
});
