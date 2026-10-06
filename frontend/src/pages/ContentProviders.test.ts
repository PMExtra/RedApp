import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { defineComponent, ref, reactive } from "vue";
import {
  boot,
  adminApplications,
  managedVendors,
  mountPage,
  resetStores,
  response,
} from "../testSupport";
import { signedIn } from "../session";
import { setLanguage } from "../i18n";
import { useNumberedCollection } from "../composables/useNumberedCollection";
import PageNavigation from "../components/PageNavigation.vue";
import VendorCard from "../components/VendorCard.vue";
const info = {
  ...adminApplications[0]!,
  provider: "info",
  base_url: "",
  cache_ttl_seconds: 0,
};
const hosted = { ...info, provider: "hosted" };
const page = (
  items: unknown[],
  total = items.length,
  current = 1,
  limit = 25,
) => ({
  items,
  total,
  page: current,
  limit,
  total_pages: Math.max(1, Math.ceil(total / limit)),
});
function common(url: string, app = info) {
  if (url === "/api/bootstrap") return response(boot);
  if (url.endsWith("/session")) return response({ csrf: "token" });
  if (url === "/admin/api/apps/openai/codex") return response({ app });
  if (url === "/admin/api/vendors/openai")
    return response({ vendor: managedVendors[0] });
  if (url === "/admin/api/providers")
    return response({
      providers: [
        {
          key: app.provider,
          name: { en: app.provider, "zh-CN": app.provider },
          description: { en: "purpose", "zh-CN": "用途" },
          capabilities: {},
        },
      ],
    });
  if (url.endsWith("/instructions"))
    return response({ en: "Original", "zh-CN": "原始", revision: 2 });
  return response({}, 404);
}
beforeEach(resetStores);
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  setLanguage("en");
  document.body.innerHTML = "";
});
it("keeps Info settings content-only, preserves instruction conflicts and does not poll", async () => {
  vi.useFakeTimers();
  const fetch = vi.fn(async (url: string, init?: RequestInit) =>
    init?.method === "PUT"
      ? response({ error: { code: "DIRECTORY_REVISION_CONFLICT" } }, 409)
      : common(url),
  );
  vi.stubGlobal("fetch", fetch);
  const { wrapper, router } = await mountPage(
    "/admin/vendors/openai/apps/codex/settings",
  );
  expect(wrapper.find("[name=base_url]").exists()).toBe(false);
  expect(wrapper.find(".ttl-form").exists()).toBe(false);
  expect(wrapper.find('a[href$="/cache"]').exists()).toBe(false);
  expect(wrapper.find('a[href$="/versions"]').exists()).toBe(false);
  expect(wrapper.find(".application-header").text()).not.toContain(
    "Copy download URL",
  );
  await wrapper
    .get("[name=instructions-en]")
    .setValue("<script>literal text</script>");
  await wrapper.get(".application-instructions-editor form").trigger("submit");
  await flushPromises();
  const write = fetch.mock.calls.find(([, init]) => init?.method === "PUT")!;
  expect(write[0]).toBe("/admin/api/apps/openai/codex/instructions");
  expect(write[1]!.headers).toMatchObject({
    "If-Match": '"2"',
    "X-CSRF-Token": "token",
  });
  expect(
    wrapper.get(".application-instructions-editor [role=alert]").text(),
  ).toContain("draft is preserved");
  const calls = fetch.mock.calls.length;
  await vi.advanceTimersByTimeAsync(15000);
  await flushPromises();
  expect(fetch.mock.calls.length).toBe(calls);
  vi.stubGlobal(
    "confirm",
    vi.fn(() => false),
  );
  await router.push("/admin/vendors");
  expect(router.currentRoute.value.path).toContain("/settings");
  expect(
    (wrapper.get("[name=instructions-en]").element as HTMLTextAreaElement)
      .value,
  ).toBe("<script>literal text</script>");
  wrapper.unmount();
});
it("embeds localized executable instruction documents without downloads for Info", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      response({
        ...boot,
        apps: [
          {
            ...boot.apps[0],
            provider: "info",
            capabilities: {
              versions: false,
              installers: false,
              time_cleanup: false,
              files: false,
            },
            instructions: {
              en: "<img src=x onerror=alert(1)>\nRead this",
              "zh-CN": "中文说明",
            },
            installers: [],
            channels: [],
          },
        ],
      }),
    ),
  );
  const { wrapper } = await mountPage("/openai/codex");
  expect(wrapper.get(".usage-instructions iframe").attributes("src")).toBe(
    "/api/apps/openai/codex/instructions/document?lang=en",
  );
  expect(
    wrapper.get(".usage-instructions iframe").attributes("sandbox"),
  ).toBeUndefined();
  expect(wrapper.find(".usage-instructions img").exists()).toBe(false);
  expect(wrapper.find(".download-prefix").exists()).toBe(false);
  expect(wrapper.find(".installation-layout").exists()).toBe(false);
  setLanguage("zh-CN");
  await flushPromises();
  expect(wrapper.get(".usage-instructions iframe").attributes("src")).toContain(
    "lang=zh-CN",
  );
  wrapper.unmount();
});
it("uploads with explicit replacement identity, shows progress and cancels without losing the draft", async () => {
  vi.useFakeTimers();
  let resolveUpload: ((v: unknown) => void) | undefined;
  const file = {
    id: "a".repeat(32),
    path: "nested/tool.zip",
    sha256: "b".repeat(64),
    size_bytes: 8,
    created_at: "2026-10-01T00:00:00Z",
  };
  const fetch = vi.fn((url: string, init?: RequestInit) => {
    if (init?.method === "POST")
      return new Promise((resolve) => (resolveUpload = resolve));
    if (init?.method === "DELETE")
      return Promise.resolve(response({ cancelled: true }));
    if (url.includes("/files/transfers/"))
      return Promise.resolve(
        response({ bytes: 4, total: 8, state: "receiving" }),
      );
    if (url.includes("/files?")) return Promise.resolve(response(page([file])));
    return Promise.resolve(common(url, hosted));
  });
  vi.stubGlobal("fetch", fetch);
  const { wrapper } = await mountPage("/admin/vendors/openai/apps/codex/files");
  await wrapper
    .findAll("button")
    .find((b) => b.text() === "Replace file")!
    .trigger("click");
  expect(
    (wrapper.get("[name=resource_path]").element as HTMLInputElement).value,
  ).toBe(file.path);
  const upload = wrapper.get("[name=hosted_file]");
  Object.defineProperty(upload.element, "files", {
    value: [new File(["newbytes"], "new.zip")],
  });
  await upload.trigger("change");
  await wrapper.get(".hosted-workspace form").trigger("submit");
  const call = fetch.mock.calls.find(([, init]) => init?.method === "POST")!;
  expect(call[1]!.body).toBeInstanceOf(FormData);
  expect((call[1]!.body as FormData).get("expected_id")).toBe(file.id);
  expect((call[1]!.body as FormData).get("path")).toBe(file.path);
  expect(call[1]!.headers).not.toHaveProperty("Content-Type");
  await vi.advanceTimersByTimeAsync(250);
  await flushPromises();
  expect(wrapper.get(".transfer-progress").text()).toContain("4.00 B");
  await wrapper.get(".transfer-progress button").trigger("click");
  await flushPromises();
  expect(call[1]!.signal?.aborted).toBe(true);
  expect(
    fetch.mock.calls.some(
      ([url, init]) => url.includes("/transfers/") && init?.method === "DELETE",
    ),
  ).toBe(true);
  resolveUpload?.(response(file));
  await flushPromises();
  expect(wrapper.text()).not.toContain("File saved.");
  expect(
    (wrapper.get("[name=resource_path]").element as HTMLInputElement).value,
  ).toBe(file.path);
  expect(wrapper.find(".transfer-progress").exists()).toBe(false);
  wrapper.unmount();
});
it("replaces an in-flight full strip with the filtered sixth application", async () => {
  signedIn.value = true;
  const apps = Array.from({ length: 6 }, (_, i) => ({
    ...adminApplications[0]!,
    uid: String(i),
    id: `item-${i}`,
    key: `openai/item-${i}`,
    name: { en: `Tool ${i + 1}`, "zh-CN": `工具${i + 1}` },
  }));
  let finish: ((value: ReturnType<typeof response>) => void) | undefined;
  const fetch = vi.fn((_url: string, _init?: RequestInit) => new Promise<ReturnType<typeof response>>((resolve) => { finish = resolve; }));
  vi.stubGlobal("fetch", fetch);
  const wrapper = mount(VendorCard, {
    props: {
      vendor: { ...managedVendors[0]!, apps: apps.slice(0, 5), app_total: 6 },
      query: "",
      state: "current",
    },
    global: { stubs: { RouterLink: { template: "<a><slot/></a>" } } },
  });
  expect(wrapper.text()).not.toContain("Tool 6");
  expect(fetch).toHaveBeenCalledTimes(1);
  await wrapper.setProps({
    query: "Tool 6",
    vendor: { ...managedVendors[0]!, apps: [apps[5]!], app_total: 1 },
  });
  await flushPromises();
  expect(fetch.mock.calls[0]![1]!.signal?.aborted).toBe(true);
  finish!(response(page(apps, 6, 1, 100)));
  await flushPromises();
  expect(wrapper.text()).toContain("Tool 6");
  expect(wrapper.text()).not.toContain("Tool 1");
  expect(fetch).toHaveBeenCalledTimes(1);
  expect(wrapper.findAll(".directory-apps li:not(.add-application)")).toHaveLength(1);
  expect(wrapper.findAll(".directory-apps li")).toHaveLength(2);
  expect(wrapper.find(".vendor-actions button").exists()).toBe(false);
  wrapper.unmount();
});
it("supports page jumps, invalid input, server clamping and bounded snapshots", async () => {
  signedIn.value = true;
  let shrink = false;
  const fetch = vi.fn(async (url: string) => {
    const n = Number(new URL(url, "https://test").searchParams.get("page"));
    return response(
      shrink
        ? page(["last"], 1, 1, 2)
        : page([`item${n}a`, `item${n}b`], 10, n, 2),
    );
  });
  vi.stubGlobal("fetch", fetch);
  const wrapper = mount(
    defineComponent({
      components: { PageNavigation },
      setup() {
        const list = reactive(useNumberedCollection<string>(ref("vendors"), 2));
        return { list };
      },
      template: `<div><p class="items">{{list.items.join(',')}}</p><PageNavigation label="Pages" :page="list.page" :total="list.total" :total-pages="list.totalPages" :previous="list.previousAvailable" :next="list.nextAvailable" :loading="list.loading" @go="list.go" @refresh="list.refresh"/></div>`,
    }),
  );
  await flushPromises();
  await wrapper.get("input").setValue("3");
  await wrapper.get("form").trigger("submit");
  await flushPromises();
  expect(wrapper.get(".items").text()).toBe("item3a,item3b");
  expect(wrapper.text()).toContain("Page 3 of 5");
  const calls = fetch.mock.calls.length;
  await wrapper.get("input").setValue("6");
  await wrapper.get("form").trigger("submit");
  expect(fetch.mock.calls.length).toBe(calls);
  expect(wrapper.get("[role=alert]").text()).toContain("1 to 5");
  shrink = true;
  await wrapper
    .findAll("button")
    .find((b) => b.attributes("aria-label") === "Refresh")!
    .trigger("click");
  await flushPromises();
  expect(wrapper.text()).toContain("Page 1 of 1");
  expect(wrapper.get(".items").text()).toBe("last");
  expect((wrapper.get("input").element as HTMLInputElement).value).toBe("1");
  wrapper.unmount();
});
