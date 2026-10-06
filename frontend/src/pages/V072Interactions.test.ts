import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import { createMemoryHistory, createRouter } from "vue-router";
import PublicSearch from "../components/PublicSearch.vue";
import TemplateReset from "../components/TemplateReset.vue";
import HomepageSettings from "../components/HomepageSettings.vue";
import {
  boot,
  mountPage,
  resetStores,
  response,
  adminApplications,
  managedVendors,
} from "../testSupport";
import { setLanguage } from "../i18n";
const list = { items: [], page: 1, total: 0, total_pages: 1, limit: 12 };
beforeEach(() => {
  resetStores();
  vi.useFakeTimers();
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  setLanguage("en");
  document.body.innerHTML = "";
});
it("preserves the focused search selection and IME across query-only navigation, but focuses real pages", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      response(
        url === "/api/bootstrap"
          ? boot
          : url.endsWith("/session")
            ? { csrf: "same-session" }
            : url.includes("/vendors?")
              ? list
              : {},
      ),
    ),
  );
  const { wrapper, router } = await mountPage("/admin/vendors");
  const input = wrapper.get(".directory-search input"),
    element = input.element as HTMLInputElement;
  element.focus();
  await input.setValue("sixth app");
  element.setSelectionRange(2, 5);
  await vi.advanceTimersByTimeAsync(260);
  await flushPromises();
  expect(router.currentRoute.value.query.q).toBe("sixth app");
  expect(document.activeElement).toBe(element);
  expect([element.selectionStart, element.selectionEnd]).toEqual([2, 5]);
  await input.trigger("compositionstart");
  element.value = "工具";
  await input.trigger("input");
  await vi.advanceTimersByTimeAsync(300);
  expect(router.currentRoute.value.query.q).toBe("sixth app");
  await input.trigger("compositionend");
  await vi.advanceTimersByTimeAsync(260);
  await flushPromises();
  expect(router.currentRoute.value.query.q).toBe("工具");
  expect(document.activeElement).toBe(element);
  await router.push("/admin/vendors/new");
  await flushPromises();
  expect(document.activeElement?.id).toBe("main-content");
  wrapper.unmount();
});
it("keeps a pending successful list response during a real visibility session recheck", async () => {
  let state = "visible",
    pending = false,
    resolveList: ((value: unknown) => void) | undefined;
  vi.spyOn(document, "visibilityState", "get").mockImplementation(
    () => state as DocumentVisibilityState,
  );
  const fetch = vi.fn((url: string) =>
    pending && url.includes("/vendors?")
      ? new Promise((resolve) => {
          resolveList = resolve;
        })
      : Promise.resolve(
          response(
            url === "/api/bootstrap"
              ? boot
              : url.endsWith("/session")
                ? { csrf: "unchanged-token" }
                : list,
          ),
        ),
  );
  vi.stubGlobal("fetch", fetch);
  const { wrapper } = await mountPage("/admin/vendors");
  state = "hidden";
  document.dispatchEvent(new Event("visibilitychange"));
  pending = true;
  state = "visible";
  document.dispatchEvent(new Event("visibilitychange"));
  await flushPromises();
  expect(
    fetch.mock.calls.filter(([url]) => url.endsWith("/session")).length,
  ).toBeGreaterThan(1);
  resolveList?.(
    response({
      ...list,
      items: [{ ...managedVendors[0], apps: [], app_total: 0 }],
      total: 1,
    }),
  );
  await flushPromises();
  expect(wrapper.get(".directory-page").text()).toContain("OpenAI");
  expect(wrapper.find(".directory-page [role=alert]").exists()).toBe(false);
  wrapper.unmount();
});
it("cancels stale suggestions, supports IME and keyboard selection, and keeps Enter/back URLs shareable", async () => {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/:pathMatch(.*)*", component: { template: "<main />" } },
    ],
  });
  await router.push("/");
  await router.isReady();
  let old: ((value: unknown) => void) | undefined;
  const fetch = vi.fn((url: string, _init?: RequestInit) =>
    url.endsWith("first")
      ? new Promise((resolve) => {
          old = resolve;
        })
      : Promise.resolve(
          response({
            items: [
              {
                kind: "vendor",
                id: "acme",
                name: { en: "Acme", "zh-CN": "示例" },
                url: "/acme",
                icon: "/assets/icons/vendor.png",
              },
              {
                kind: "app",
                id: "acme/tool",
                name: { en: "Tool", "zh-CN": "工具" },
                url: "/acme/tool",
                icon: "/assets/icons/tool.png",
              },
              {
                kind: "app",
                id: "acme/plain",
                name: { en: "Plain", "zh-CN": "默认" },
                url: "/acme/plain",
                icon: "",
              },
            ],
          }),
        ),
  );
  vi.stubGlobal("fetch", fetch);
  const wrapper = mount(PublicSearch, {
    global: { plugins: [router] },
    attachTo: document.body,
  });
  const input = wrapper.get("input");
  (input.element as HTMLInputElement).focus();
  await input.setValue("first");
  await vi.advanceTimersByTimeAsync(210);
  const first = fetch.mock.calls[0]![1]!;
  await input.setValue("second");
  expect(first.signal?.aborted).toBe(true);
  await vi.advanceTimersByTimeAsync(210);
  await flushPromises();
  old?.(
    response({
      items: [
        {
          kind: "app",
          id: "wrong/app",
          name: { en: "Wrong" },
          url: "/wrong/app",
        },
      ],
    }),
  );
  await flushPromises();
  expect(wrapper.text()).not.toContain("Wrong");
  const options = wrapper.findAll('[role="option"]');
  expect(options[0]!.get("img").attributes("src")).toBe(
    "/assets/icons/vendor.png",
  );
  expect(options[1]!.get("img").attributes("src")).toBe(
    "/assets/icons/tool.png",
  );
  expect(options[1]!.text()).toContain("Application");
  expect(options[2]!.get(".entity-icon svg").attributes("aria-hidden")).toBe(
    "true",
  );
  await options[1]!.get("img").trigger("error");
  expect(options[1]!.find("img").exists()).toBe(false);
  expect(options[1]!.get(".entity-icon svg").html()).toBe(
    options[2]!.get(".entity-icon svg").html(),
  );

  await input.trigger("keydown", { key: "ArrowDown" });
  expect(wrapper.get("[role=option]").attributes("aria-selected")).toBe(
    "true",
  );
  await input.trigger("keydown", { key: "Enter" });
  await flushPromises();
  expect(router.currentRoute.value.path).toBe("/acme");
  await input.trigger("compositionstart");
  await input.setValue("中文");
  await input.trigger("keydown", { key: "Enter", isComposing: true });
  expect(router.currentRoute.value.path).toBe("/acme");
  await input.trigger("compositionend");
  await vi.advanceTimersByTimeAsync(210);
  await flushPromises();
  await input.trigger("keydown", { key: "Enter" });
  await flushPromises();
  expect(router.currentRoute.value.path).toBe("/all");
  expect(router.currentRoute.value.query.q).toBe("中文");
  expect(wrapper.find("[role=listbox]").exists()).toBe(false);
  await router.push("/all?q=other");
  await flushPromises();
  router.back();
  await vi.advanceTimersByTimeAsync(1);
  await flushPromises();
  expect((input.element as HTMLInputElement).value).toBe("中文");
  wrapper.unmount();
});
it("starts template reset unselected, reviews selected differences and preserves selection on a CAS conflict", async () => {
  const current = {
    ...adminApplications[0]!,
    name: { en: "Custom name", "zh-CN": "自定义" },
    revision: 9,
  };
  const preview = {
    current,
    instructions: { en: "Custom text", "zh-CN": "自定义说明", revision: 4 },
    template: {
      application: {
        ...current,
        name: { en: "Template name", "zh-CN": "模板" },
        enabled: false,
      },
      instructions: { en: "Template text", "zh-CN": "模板说明" },
    },
    groups: ["metadata", "instructions_en", "enabled"],
  };
  const fetch = vi.fn(async (_url: string, init?: RequestInit) =>
    init?.method === "POST"
      ? response({ error: { code: "DIRECTORY_REVISION_CONFLICT" } }, 409)
      : response(preview),
  );
  vi.stubGlobal("fetch", fetch);
  const wrapper = mount(TemplateReset, {
    props: { application: current.key },
  });
  await flushPromises();
  expect(wrapper.element.tagName).toBe("DETAILS");
  expect(wrapper.attributes("open")).toBeUndefined();
  await wrapper.get("summary").trigger("click");
  const callsBeforeSelection = fetch.mock.calls.length;
  await wrapper.get('[aria-label="Enabled"]').trigger("click");
  expect(fetch.mock.calls.length).toBe(callsBeforeSelection);
  await wrapper.get('[aria-label="Enabled"]').trigger("click");
  expect(wrapper.findAll("[role=switch][aria-checked=true]")).toHaveLength(0);
  expect(
    wrapper.find("button:not([role=switch])").attributes("disabled"),
  ).toBeDefined();
  await wrapper.get('[aria-label="Name and description"]').trigger("click");
  await wrapper
    .findAll("button")
    .find((b) => b.text() === "Review differences")!
    .trigger("click");
  expect(wrapper.get(".template-diff").text()).toContain("Custom name");
  expect(wrapper.get(".template-diff").text()).toContain("Template name");
  expect(wrapper.get(".template-diff").text()).not.toContain("Custom text");
  await wrapper
    .findAll("button")
    .find((b) => b.text() === "Save selected fields")!
    .trigger("click");
  await flushPromises();
  const write = fetch.mock.calls.find(([, init]) => init?.method === "POST")!;
  expect(JSON.parse(write[1]!.body as string)).toEqual({
    revision: 9,
    instructions_revision: 4,
    groups: ["metadata"],
  });
  expect(wrapper.get("[role=alert]").text()).toContain("draft is preserved");
  expect(
    wrapper
      .get('[aria-label="Name and description"]')
      .attributes("aria-checked"),
  ).toBe("true");
  wrapper.unmount();
});

it("saves pinned applications in the handle-selected order only after explicit save", async () => {
  const fetch = vi.fn(async (_url: string, init?: RequestInit) =>
    response(
      init?.method === "PUT"
        ? { ...JSON.parse(init.body as string), revision: 5 }
        : { keys: ["a/one", "b/two", "c/three"], revision: 4 },
    ),
  );
  vi.stubGlobal("fetch", fetch);
  const wrapper = mount(HomepageSettings, { attachTo: document.body });
  await flushPromises();
  await wrapper
    .findAll(".sort-handle")[0]!
    .trigger("keydown", { key: "End" });
  expect(wrapper.findAll(".pinned-order code").map((n) => n.text())).toEqual([
    "b/two",
    "c/three",
    "a/one",
  ]);
  expect(document.activeElement).toBe(
    wrapper.findAll(".sort-handle")[2]!.element,
  );
  expect(
    fetch.mock.calls.filter(([, init]) => init?.method === "PUT"),
  ).toHaveLength(0);
  await wrapper.get('[aria-label="Remove: b/two"]').trigger("click");
  await wrapper.get('[aria-label="Application key"]').setValue("d/four");
  await wrapper.get('[aria-label="Add application"]').trigger("click");
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  const write = fetch.mock.calls.find(([, init]) => init?.method === "PUT")!;
  expect(write[1]!.headers).toMatchObject({ "If-Match": '\"4\"' });
  expect(JSON.parse(write[1]!.body as string)).toEqual({
    keys: ["c/three", "a/one", "d/four"],
  });
  wrapper.unmount();
});

it("filters vendors with three buttons, resets pages, preserves search and normalizes a removed deleted view", async () => {
  const fetch = vi.fn(async (url: string) => {
    if (url === "/api/bootstrap") return response(boot);
    if (url.endsWith("/session")) return response({ csrf: "token" });
    const params = new URL(url, "https://test").searchParams;
    const disabled = params.get("state") === "disabled";
    return response({
      ...list,
      page: Number(params.get("page") || 1),
      total: 25,
      total_pages: 3,
      items: [
        {
          ...managedVendors[0]!,
          enabled: !disabled,
          apps: [adminApplications[0]!],
          app_total: 1,
        },
      ],
    });
  });
  vi.stubGlobal("fetch", fetch);
  const { wrapper, router } = await mountPage(
    "/admin/vendors?q=Tool&state=deleted",
  );
  await flushPromises();
  const buttons = () => wrapper.findAll(".status-filter button");
  expect(buttons().map((b) => b.text())).toEqual([
    "All",
    "Enabled",
    "Disabled",
  ]);
  expect(wrapper.find(".directory-toolbar .field-label").exists()).toBe(
    false,
  );
  expect(router.currentRoute.value.query.state).toBeUndefined();
  expect(buttons()[0]!.attributes("aria-pressed")).toBe("true");
  await wrapper.get('[aria-label="Next page"]').trigger("click");
  await flushPromises();
  expect(
    new URL(fetch.mock.calls.at(-1)![0], "https://test").searchParams.get(
      "page",
    ),
  ).toBe("2");
  await buttons()[2]!.trigger("click");
  await flushPromises();
  const params = new URL(fetch.mock.calls.at(-1)![0], "https://test")
    .searchParams;
  expect([params.get("q"), params.get("state"), params.get("page")]).toEqual([
    "Tool",
    "disabled",
    "1",
  ]);
  expect(wrapper.get(".vendor-card").text()).toContain("Disabled by vendor");
  expect(buttons()[2]!.attributes("aria-pressed")).toBe("true");
  await router.push("/admin/vendors?q=Next&state=enabled");
  await flushPromises();
  expect(buttons()[1]!.attributes("aria-pressed")).toBe("true");
  router.back();
  await vi.advanceTimersByTimeAsync(1);
  await flushPromises();
  expect(buttons()[2]!.attributes("aria-pressed")).toBe("true");
  expect(
    (wrapper.get(".directory-search input").element as HTMLInputElement)
      .value,
  ).toBe("Tool");
  wrapper.unmount();
});
