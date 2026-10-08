import { selectValue } from "../testSupport";
import { flushPromises } from "@vue/test-utils";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  adminApplications,
  managedVendors,
  boot,
  mountPage,
  resetStores,
  response,
 configurationFixture,applyConfigurationPatch,
} from "../testSupport";
import type {
  ManagedApplication,
  ProviderDefinition,
  Vendor,
} from "../directory";
import { setLanguage } from "../i18n";

const providers: ProviderDefinition[] = [
  {
    key: "http-cache",
    name: { en: "HTTP Cache", "zh-CN": "HTTP 缓存" },
    default_base_url: "",
    capabilities: { versions: false, installers: false, time_cleanup: true },
  },
  {
    key: "codex",
    name: { en: "Codex", "zh-CN": "Codex" },
    default_base_url: "https://releases.openai.com/codex/",
    capabilities: { versions: true, installers: true, time_cleanup: false },
  },
  {
    key: "claude-code",
    name: { en: "Claude Code", "zh-CN": "Claude Code" },
    default_base_url: "https://downloads.claude.ai/claude-code-releases/",
    capabilities: { versions: true, installers: true, time_cleanup: false },
  },
  {
    key: "info",
    name: { en: "App Info", "zh-CN": "应用介绍" },
    default_base_url: "",
    capabilities: { versions: false, installers: false, time_cleanup: false },
  },
  {
    key: "hosted",
    name: { en: "Hosted Files", "zh-CN": "文件托管" },
    default_base_url: "",
    capabilities: { versions: false, installers: false, time_cleanup: false },
  },
];
let vendors: Vendor[], apps: ManagedApplication[];
beforeEach(() => {
  resetStores();
  vendors = structuredClone(managedVendors);
  apps = structuredClone(adminApplications);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  setLanguage("en");
  document.body.innerHTML = "";
});
function read(url: string) {
 if(url.endsWith('/configuration')){const base=url.slice(0,-14);const row=apps.find(a=>base===`/admin/api/apps/${a.key}`)||vendors.find(v=>base===`/admin/api/vendors/${v.id}`);return row?response(configurationFixture(row as unknown as Record<string,unknown>,row.revision,row.has_template||('builtin_template' in row&&row.builtin_template)?('key' in row?row.key:row.id):null)):response({},404);}

  if (url === "/api/bootstrap") return response(boot);
  if (url.endsWith("/session")) return response({ csrf: "directory-token" });
  if (new URL(url, "https://test").pathname === "/admin/api/vendors") {
    const deleted =
      new URL(url, "https://test").searchParams.get("state") === "deleted";
    const items = vendors
      .map((v) => ({
        ...v,
        apps: apps.filter(
          (a) => a.vendor_id === v.id && !!a.deleted_at === deleted,
        ),
        app_total: apps.filter(
          (a) => a.vendor_id === v.id && !!a.deleted_at === deleted,
        ).length,
      }))
      .filter((v) => !deleted || v.app_total > 0);
    return response({
      items,
      page: 1,
      limit: 12,
      total: items.length,
      total_pages: 1,
    });
  }
  if (url === "/admin/api/apps")
    return response({ apps: structuredClone(apps) });
  if (url === "/admin/api/providers") return response({ providers });
  if (url.endsWith("/sources"))
    return response({
      sources: [
        {
          epoch: 1,
          base_url: "https://upstream.example/releases/",
          current: true,
          active: true,
          created_at: "2026-10-01T00:00:00Z",
        },
      ],
    });
  if (url.endsWith("/cache/policy"))
    return response({
      revision: 1,
      stale_fallback: true,
      rules: [],
      auto_cleanup: [],
    });
  const app = apps.find((item) => url === `/admin/api/apps/${item.key}`);
  if (app) return response({ app: structuredClone(app) });
  const vendor = vendors.find(
    (item) => url === `/admin/api/vendors/${item.id}`,
  );
  if (vendor) return response({ vendor: structuredClone(vendor) });
  if (url.endsWith("/instructions"))
    return response({ en: "", "zh-CN": "", revision: 0 });
  if (url.endsWith("/settings"))
    return response({ channel_ttl_seconds: 60, revision: 0 });
  return response({}, 404);
}

it("creates a bilingual vendor and application with explicit provider defaults and CSRF-protected icon upload", async () => {
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === "POST" && url === "/admin/api/assets/icons")
      return response({ icon: "/assets/icons/example.png" });
    if (init?.method === "POST" && url === "/admin/api/vendors") {
      const vendor = {
        ...JSON.parse(init.body as string),
        uid: "vendor-acme",
        revision: 1,
      } as Vendor;
      vendors.push(vendor);
      return response({ vendor });
    }
    if (init?.method === "POST" && url === "/admin/api/vendors/acme/apps") {
      const fields = JSON.parse(init.body as string);
      const app = {
        ...fields,
        base_url: fields.base_urls?.[0] || fields.base_url,
        uid: "app-tools",
        key: `acme/${fields.id}`,
        vendor_id: "acme",
        vendor_uid: "vendor-acme",
        revision: 1,
        source_epoch: 1,
      } as ManagedApplication;
      apps.push(app);
      return response({ app });
    }
    return read(url);
  });
  vi.stubGlobal("fetch", fetch);
  const { wrapper, router } = await mountPage("/admin/vendors/new");
  async function checkHeaderSwitch() {
    const editor = wrapper.get(".directory-editor");
    expect(editor.findAll('[name="enabled"]')).toHaveLength(1);
    const toggle = editor.get('.entity-editor-heading [role="switch"]');
    const initial = toggle.attributes("aria-checked");
    const calls = fetch.mock.calls.length;
    await toggle.trigger("click");
    expect(toggle.attributes("aria-checked")).not.toBe(initial);
    expect(fetch.mock.calls.length).toBe(calls);
    await toggle.trigger("click");
    expect(toggle.attributes("aria-checked")).toBe(initial);
  }
  await checkHeaderSwitch();
  await wrapper.get('[name="id"]').setValue("acme");
  await wrapper.get('[name="name-en"]').setValue("Acme");
  await wrapper.get('[name="name-zh-CN"]').setValue("示例厂商");
  await wrapper.get('[name="description-en"]').setValue("Tools for teams");
  const upload = wrapper.get('input[type="file"]');
  Object.defineProperty(upload.element, "files", {
    value: [new File(["fixture"], "icon.png", { type: "image/png" })],
  });
  await upload.trigger("change");
  await flushPromises();
  const iconRequest = fetch.mock.calls.find(([url]) =>
    url.endsWith("/assets/icons"),
  )![1]!;
  expect(iconRequest.body).toBeInstanceOf(FormData);
  expect(iconRequest.headers).toMatchObject({
    "X-CSRF-Token": "directory-token",
  });
  expect(iconRequest.headers).not.toHaveProperty("Content-Type");
  await wrapper.get(".directory-editor form").trigger("submit");
  await flushPromises();
  await flushPromises();
  await vi.waitFor(() => expect(router.currentRoute.value.path).toBe("/admin/vendors/acme/settings"), {timeout:5000});
  await flushPromises();
  expect(vendors.at(-1)?.name["zh-CN"]).toBe("示例厂商");
  expect(vendors.at(-1)?.icon).toBe("/assets/icons/example.png");
  expect(wrapper.get('[name="id"]').attributes("disabled")).toBeDefined();
  await router.push("/admin/vendors/acme/apps/new");
  await flushPromises();
  await checkHeaderSwitch();
  expect(wrapper.find('[name="base_url"]').exists()).toBe(false);
  async function selectProvider(name: string) {
    await wrapper.get('[aria-label="Provider"]').trigger("click");
    await wrapper
      .findAll('[role="option"]')
      .find((option) => option.text() === name)!
      .trigger("click");
  }
  await selectProvider("Codex");
  expect(
    (wrapper.get('[name="base_url"]').element as HTMLInputElement).value,
  ).toBe(providers[1]!.default_base_url);
  await selectProvider("Claude Code");
  expect(
    (wrapper.get('[name="base_url"]').element as HTMLInputElement).value,
  ).toBe(providers[2]!.default_base_url);
  await selectProvider("HTTP Cache");
  expect(
    (wrapper.get('[name="base_url"]').element as HTMLInputElement).value,
  ).toBe("");
  await wrapper
    .get('[name="base_url"]')
    .setValue("http://packages.internal/tools/");
  await wrapper.get('[name="id"]').setValue("tools");
  await wrapper.get('[name="name-en"]').setValue("Tools");
  await wrapper.get('[name="name-zh-CN"]').setValue("工具");
  await wrapper.get(".directory-editor form").trigger("submit");
  await flushPromises();
  await flushPromises();
  await vi.waitFor(() =>
    expect(router.currentRoute.value.path, wrapper.find(".directory-editor .error").exists() ? wrapper.find(".directory-editor .error").text() : "waiting for saved application navigation").toBe(
      "/admin/vendors/acme/apps/tools/cache",
    ),
    {timeout:5000},
  );
  await router.push("/admin/vendors/acme/apps/tools/settings");
  await flushPromises();
  expect(
    wrapper.get('[aria-label="Provider"]').attributes("disabled"),
  ).toBeDefined();
  expect(apps.at(-1)).toMatchObject({
    provider: "http-cache",
    base_url: "http://packages.internal/tools/",
    base_urls: ["http://packages.internal/tools/"],
    source_strategy: "ordered",
    cache_ttl_seconds: 300,
  });
  expect(wrapper.find(".ttl-form").exists()).toBe(false);
  expect(
    wrapper.find('a[href="/admin/vendors/acme/apps/tools/versions"]').exists(),
  ).toBe(false);
  wrapper.unmount();
});

it("preserves a revision-conflict draft, guards navigation and discards an old application's late load", async () => {
  let delayed = false,
    resolveOld: ((value: unknown) => void) | undefined;
  const fetch = vi.fn((url: string, init?: RequestInit) => {
    if (init?.method === "PATCH")
      return Promise.resolve(
        response({ error: { code: "DIRECTORY_REVISION_CONFLICT" } }, 409),
      );
    if (delayed && url === "/admin/api/apps/openai/codex")
      return new Promise((resolve) => {
        resolveOld = resolve;
      });
    return Promise.resolve(read(url));
  });
  vi.stubGlobal("fetch", fetch);
  const confirm = vi.fn().mockReturnValue(false);
  vi.stubGlobal("confirm", confirm);
  const { wrapper, router } = await mountPage(
    "/admin/vendors/openai/apps/codex/settings",
  );
  await wrapper.get('[name="name-en"]').setValue("Unsaved Codex");
  await wrapper.get(".directory-editor form").trigger("submit");
  await flushPromises();
  const request = fetch.mock.calls.find(
    ([, init]) => init?.method === "PATCH",
  )![1]!;
  expect(JSON.parse(request.body as string)).toMatchObject({
    revision: 1,
    set: { "name.en": "Unsaved Codex" },
  });
  expect(JSON.parse(request.body as string)).not.toHaveProperty("id");
  expect(JSON.parse(request.body as string)).not.toHaveProperty("provider");
  expect(wrapper.get(".directory-editor [role=alert]").text()).toContain(
    "Your draft is preserved",
  );
  await router.push("/admin/vendors/anthropic/apps/claude-code/settings");
  expect(router.currentRoute.value.path).toBe(
    "/admin/vendors/openai/apps/codex/settings",
  );
  expect(
    (wrapper.get('[name="name-en"]').element as HTMLInputElement).value,
  ).toBe("Unsaved Codex");
  confirm.mockReturnValue(true);
  delayed = true;
  await wrapper
    .get(".directory-editor")
    .findAll("button")
    .find((button) => button.attributes("aria-label") === "Reload")!
    .trigger("click");
  await router.push("/admin/vendors/anthropic/apps/claude-code/settings");
  await flushPromises();
  resolveOld?.(
    response({
      app: {
        ...apps[0],
        name: { en: "Late wrong app", "zh-CN": "错误应用" },
      },
    }),
  );
  await flushPromises();
  expect(
    (wrapper.get('[name="name-en"]').element as HTMLInputElement).value,
  ).toBe("Claude Code");
  expect(wrapper.text()).not.toContain("Late wrong app");
  expect(wrapper.find(".directory-editor [role=alert]").exists()).toBe(false);
  wrapper.unmount();
});

it("keeps disabled applications manageable and protects deletion while hiding deleted filters", async () => {
  apps[0]!.enabled = false;
  apps[0]!.id = "custom";
  apps[0]!.key = "openai/custom";
  apps[1]!.deleted_at = "2026-10-03T12:00:00Z";
  let deletionAttempts = 0;
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    if (url === "/api/bootstrap") return response({ ...boot, apps: [] });
    if (init?.method === "DELETE") {
      expect(JSON.parse(init.body as string)).toEqual({
        revision: 1,
        confirm_key: "openai/custom",
        confirm_uid: apps[0]!.uid,
      });
      if (++deletionAttempts === 1)
        return response(
          { error: { code: "DIRECTORY_DELETE_PENDING", retryable: true } },
          409,
        );
      apps[0]!.deleted_at = "2026-10-03T13:00:00Z";
      apps[0]!.revision++;
      apps.splice(0, 1);
      return response({ deleted: true, cleanup_pending: false });
    }
    return read(url);
  });
  vi.stubGlobal("fetch", fetch);
  const { wrapper, router } = await mountPage("/admin/vendors");
  expect(wrapper.get(".directory-page").text()).toContain("Codex CLI");
  expect(wrapper.get(".directory-page").text()).toContain("Disabled");
  expect(wrapper.get(".directory-page").text()).not.toContain("Claude Code");
  expect(wrapper.find(".directory-toolbar [role=combobox]").exists()).toBe(
    false,
  );
  expect(
    wrapper.findAll(".status-filter button").map((b) => b.text()),
  ).toEqual(["All", "Enabled", "Disabled"]);
  await router.push("/admin/vendors/openai/apps/custom/settings");
  await flushPromises();
  expect(wrapper.find('[name="name-en"]').exists()).toBe(true);
  await wrapper
    .get(".directory-editor")
    .findAll("button")
    .find((button) => button.attributes("aria-label") === "Delete")!
    .trigger("click");
  expect(wrapper.get(".delete-review").text()).toContain("cannot be undone");
  expect(wrapper.get(".delete-review").text()).toContain(
    "will be interrupted",
  );
  await wrapper.get(".delete-review .danger").trigger("click");
  await flushPromises();
  expect(wrapper.get(".directory-editor [role=alert]").text()).toContain(
    "Retry deletion; restarting also resumes it.",
  );
  expect(wrapper.get(".directory-editor [role=alert]").text()).not.toMatch(
    /reload/i,
  );
  expect(router.currentRoute.value.path).toBe(
    "/admin/vendors/openai/apps/custom/settings",
  );
  expect(
    wrapper.get(".delete-review .danger").attributes("disabled"),
  ).toBeUndefined();
  expect(deletionAttempts).toBe(1);
  // The explicit retry succeeds without reloading or discarding the confirmation.
  await wrapper.get(".delete-review .danger").trigger("click");
  await flushPromises();
  expect(deletionAttempts).toBe(2);
  expect(router.currentRoute.value.path).toBe("/admin/vendors");
  expect(wrapper.get(".directory-page").text()).not.toContain("Codex CLI");
  wrapper.unmount();
});

it("presents GeneralHttp as a download prefix without installer commands or vendor-supplied markup", async () => {
  const general = {
    ...boot.apps[0]!,
    id: "acme/files",
    provider: "http-cache",
    capabilities: { ...providers[0]!.capabilities, files: true },
    name: { en: "<script>Files</script>", "zh-CN": "文件" },
    installers: [],
    channels: [],
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => response({ ...boot, apps: [general] })),
  );
  const { wrapper } = await mountPage("/acme/files");
  expect(wrapper.get(".download-prefix pre").text()).toBe(
    "https://downloads.example:8443/acme/files/",
  );
  expect(wrapper.find(".installation-layout").exists()).toBe(false);
  expect(wrapper.find("script").exists()).toBe(false);
  expect(wrapper.text()).toContain("<script>Files</script>");
  wrapper.unmount();
});

it("reorders HTTP-cache upstreams with the handle keyboard and pointer controls and saves only the canonical list and strategy", async () => {
  apps[0] = {
    ...apps[0]!,
    provider: "http-cache",
    base_url: "https://first.example/releases/",
    base_urls: [
      "https://first.example/releases/",
      "https://second.example/releases/",
    ],
    source_strategy: "ordered",
  };
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === "PATCH") {
      const changes = JSON.parse(init.body as string).set;
      apps[0] = {
        ...apps[0]!,
        ...changes,
        base_url: changes.base_urls[0],
        revision: 2,
        source_epoch: 2,
      };
      return response(configurationFixture(apps[0] as unknown as Record<string,unknown>,2));
    }
    return read(url);
  });
  vi.stubGlobal("fetch", fetch);
  const { wrapper } = await mountPage(
    "/admin/vendors/openai/apps/codex/settings",
  );
  const editor = wrapper.get(".directory-editor");
  expect(editor.text()).toContain("Default TTL without Cache-Control");
  expect(editor.text()).toContain("1 to 16");
  await editor
    .findAll("button")
    .find((button) => button.attributes("aria-label") === "Add source")!
    .trigger("click");
  await editor
    .get('[name="base_url_3"]')
    .setValue("https://third.example/releases/");
  const rows = () => editor.findAll(".source-url-list [data-sortable-row]");
  await rows()[0]!
    .get(".sort-handle")
    .trigger("keydown", { key: "ArrowDown" });
  expect((rows()[0]!.get("input").element as HTMLInputElement).value).toBe(
    "https://second.example/releases/",
  );
  vi.spyOn(document, "elementFromPoint").mockReturnValue(rows()[0]!.element);
  await rows()[2]!
    .get(".sort-handle")
    .trigger("pointerdown", {
      pointerId: 1,
      button: 0,
      isPrimary: true,
      clientX: 10,
      clientY: 100,
    });
  await editor
    .get(".sortable-list")
    .trigger("pointermove", { pointerId: 1, clientX: 10, clientY: 60 });
  await editor.get(".sortable-list").trigger("pointerup", { pointerId: 1 });
  expect(
    rows().map((row) => (row.get("input").element as HTMLInputElement).value),
  ).toEqual([
    "https://third.example/releases/",
    "https://second.example/releases/",
    "https://first.example/releases/",
  ]);
  await selectValue(editor.get('[name="source_strategy"]'), "round_robin");
  await editor.get("form").trigger("submit");
  await flushPromises();
  const request = fetch.mock.calls.find(
    ([, init]) => init?.method === "PATCH",
  )![1]!;
  const sent = JSON.parse(request.body as string);
  expect(sent.set.base_urls).toEqual([
    "https://third.example/releases/",
    "https://second.example/releases/",
    "https://first.example/releases/",
  ]);
  expect(sent.set.source_strategy).toBe("round_robin");
  expect(sent.revision).toBe(1);
  expect(sent).not.toHaveProperty("base_url");
  expect(sent).not.toHaveProperty("provider");
  expect(apps[0].source_epoch).toBe(2);
  wrapper.unmount();
});

it.each(["vendor", "app"] as const)("immediately saves existing %s enabled state without submitting or clearing other drafts", async (kind) => {
  const item = kind === "vendor" ? vendors[0]! : apps[0]!;
  const target = kind === "vendor" ? `/admin/api/vendors/${item.id}` : `/admin/api/apps/${(item as ManagedApplication).key}`;
  let complete: ((value: ReturnType<typeof response>) => void) | undefined;
  const writes: Record<string, unknown>[] = [];
  const fetch = vi.fn((url: string, init?: RequestInit) => {
    if ((url === target||url===`${target}/configuration`) && init?.method === "PATCH") {
      const body = JSON.parse(init.body as string);
      writes.push(body);
      if (writes.length === 1) return new Promise<ReturnType<typeof response>>((resolve) => { complete = resolve; });
      if(url.endsWith("/configuration"))return Promise.resolve(response(applyConfigurationPatch(item,body)));
      Object.assign(item, body, { revision: item.revision + 1 });
      return Promise.resolve(response({ [kind]: structuredClone(item) }));
    }
    return Promise.resolve(read(url));
  });
  vi.stubGlobal("fetch", fetch);
  const { wrapper } = await mountPage(kind === "vendor" ? `/admin/vendors/${item.id}/settings` : `/admin/vendors/${(item as ManagedApplication).vendor_id}/apps/${item.id}/settings`);
  const name = wrapper.get('[name="name-en"]');
  await name.setValue("Keep this draft");
  const toggle = wrapper.get('.directory-editor [name="enabled"]');
  const previous = item.enabled, revision = item.revision;
  await toggle.trigger("click");
  await flushPromises();
  expect(writes).toEqual([{ revision, enabled: !previous }]);
  expect(toggle.attributes("disabled")).toBeDefined();
  await toggle.trigger("click");
  await wrapper.get(".directory-editor form").trigger("submit");
  expect(writes).toHaveLength(1);
  Object.assign(item, { enabled: !previous, revision: revision + 1 });
  complete!(response({ [kind]: structuredClone(item) }));
  await flushPromises();
  expect((name.element as HTMLInputElement).value).toBe("Keep this draft");
  expect(toggle.attributes("aria-checked")).toBe(String(!previous));
  await wrapper.get(".directory-editor form").trigger("submit");
  await flushPromises();
  expect(writes[1]).toMatchObject({ revision: revision + 1, set: { "name.en": "Keep this draft" } });
  expect(writes[1]).not.toHaveProperty("enabled");
  expect(item.enabled).toBe(!previous);
  wrapper.unmount();
});

it.each([409, 500])("rolls back a failed immediate toggle (%s) while preserving dirty fields", async (status) => {
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => init?.method === "PATCH" ? response({ error: { code: status === 409 ? "DIRECTORY_REVISION_CONFLICT" : "INTERNAL_ERROR" } }, status) : read(url)));
  const { wrapper } = await mountPage("/admin/vendors/openai/apps/codex/settings");
  await wrapper.get('[name="name-en"]').setValue("Keep me");
  const toggle = wrapper.get('.directory-editor [name="enabled"]');
  const previous = toggle.attributes("aria-checked");
  await toggle.trigger("click");
  await flushPromises();
  expect(toggle.attributes("aria-checked")).toBe(previous);
  expect(toggle.attributes("disabled")).toBeUndefined();
  expect(wrapper.get(".directory-editor [role=alert]").text()).not.toBe("");
  expect((wrapper.get('[name="name-en"]').element as HTMLInputElement).value).toBe("Keep me");
  wrapper.unmount();
});

it("ignores an immediate-toggle response after switching applications", async () => {
  let complete: ((value: ReturnType<typeof response>) => void) | undefined;
  vi.stubGlobal("confirm", () => true);
  vi.stubGlobal("fetch", vi.fn((url: string, init?: RequestInit) => init?.method === "PATCH" ? new Promise<ReturnType<typeof response>>((resolve) => { complete = resolve; }) : Promise.resolve(read(url))));
  const { wrapper, router } = await mountPage("/admin/vendors/openai/apps/codex/settings");
  await wrapper.get('.directory-editor [name="enabled"]').trigger("click");
  await flushPromises();
  await router.push("/admin/vendors/anthropic/apps/claude-code/settings");
  await flushPromises();
  const name = (wrapper.get('[name="name-en"]').element as HTMLInputElement).value;
  complete!(response({ app: { ...apps[0], enabled: false, revision: 20 } }));
  await flushPromises();
  expect((wrapper.get('[name="name-en"]').element as HTMLInputElement).value).toBe(name);
  expect(wrapper.get('[name="id"]').element).toHaveProperty("value", "claude-code");
  wrapper.unmount();
});

it("lists every vendor application across pages including disabled records, with vendor-scoped navigation and add context", async () => {
  const original = apps[0]!;
  apps = Array.from({ length: 23 }, (_, i) => ({ ...structuredClone(original), uid: `app-${i}`, id: `tool-${i}`, key: `openai/tool-${i}`, provider: "info" as const, enabled: i % 2 === 0, name: { en: `Tool ${i}`, "zh-CN": `工具 ${i}` } }));
  apps.push(structuredClone(adminApplications[1]!));
  const fetch = vi.fn(async (url: string) => {
    const u = new URL(url, "https://test");
    const vendor = u.pathname.match(/^\/admin\/api\/vendors\/([^/]+)\/apps$/)?.[1];
    if (vendor) {
      expect(u.searchParams.get("state")).toBe("current");
      const page = Number(u.searchParams.get("page")), limit = Number(u.searchParams.get("limit"));
      const items = apps.filter((a) => a.vendor_id === vendor);
      return response({ items: items.slice((page - 1) * limit, page * limit), page, limit, total: items.length, total_pages: Math.max(1, Math.ceil(items.length / limit)) });
    }
    return read(url);
  });
  vi.stubGlobal("fetch", fetch);
  const { wrapper, router } = await mountPage("/admin/vendors/openai/apps");
  const list = () => wrapper.get(".vendor-applications");
  expect(wrapper.get(".vendor-header h1").text()).toBe("OpenAI");
  expect(wrapper.get('.application-tabs a[aria-current="page"]').text()).toBe("Applications");
  expect(list().findAll("tbody tr")).toHaveLength(20);
  expect(list().text()).toContain("Tool 1");
  expect(list().text()).toContain("Disabled");
  expect(list().text()).not.toContain("Claude Code");
  const found = new Set(list().findAll("tbody tr td:first-child a").map((a) => a.attributes("href")));
  await list().get('[aria-label="Next page"]').trigger("click");
  await flushPromises();
  expect(list().findAll("tbody tr")).toHaveLength(3);
  list().findAll("tbody tr td:first-child a").forEach((a) => found.add(a.attributes("href")));
  expect(found.size).toBe(23);
  await list().get('a[href="/admin/vendors/openai/apps/tool-22/settings"]').trigger("click");
  await flushPromises();
  expect(router.currentRoute.value.path).toBe("/admin/vendors/openai/apps/tool-22/settings");
  await wrapper.get('.breadcrumbs a[href="/admin/vendors/openai/apps"]').trigger("click");
  await flushPromises();
  await list().get('[aria-label="Add application"]').trigger("click");
  await flushPromises();
  expect(router.currentRoute.value.path).toBe("/admin/vendors/openai/apps/new");
  expect((wrapper.get('[name="id"]').element as HTMLInputElement).value).toBe("");
  await wrapper.get('.directory-editor .breadcrumbs a').trigger("click");
  await flushPromises();
  await wrapper.get('.application-tabs a[href$="/settings"]').trigger("click");
  await flushPromises();
  expect(wrapper.find(".directory-editor").exists()).toBe(true);
  expect(wrapper.find('.directory-editor a[href$="/apps/new"]').exists()).toBe(false);
  router.back();
  await vi.waitFor(() => expect(router.currentRoute.value.path).toBe("/admin/vendors/openai/apps"));
  await flushPromises();
  expect(list().text()).toContain("Tool 1");
  await router.push("/admin/vendors/anthropic/apps");
  await flushPromises();
  expect(wrapper.get(".vendor-header h1").text()).toBe("Anthropic");
  expect(list().text()).toContain("Claude Code");
  expect(list().text()).not.toContain("Tool 1");
  expect(list().get('[aria-label="Add application"]').attributes("href")).toBe("/admin/vendors/anthropic/apps/new");
  wrapper.unmount();
});

it("loads all vendor apps into a single strip and links filtered counts to the vendor tab", async () => {
  const original = apps[0]!;
  apps = Array.from({ length: 8 }, (_, i) => ({ ...structuredClone(original), uid: `preview-${i}`, id: `preview-${i}`, key: `openai/preview-${i}`, provider: "info" as const, enabled: false, name: { en: i === 0 ? "A very long application title that must remain accessible after two visible lines" : `Preview ${i}`, "zh-CN": `应用预览 ${i}` } }));
  let previewTotal = 8;
  const fetch = vi.fn(async (url: string) => {
    const u = new URL(url, "https://test");
    if (u.pathname === "/admin/api/vendors") return response({ items: [{ ...vendors[0]!, apps: apps.slice(0, 5), app_total: previewTotal }], page: 1, total: 13, total_pages: 2 });
    if (u.pathname === "/admin/api/vendors/openai/apps") {
      const filtered = u.searchParams.get("q") === "Preview" && u.searchParams.get("state") === "disabled";
      return response({ items: filtered ? apps.slice(0, 8) : apps, page: 1, total: apps.length, total_pages: 1 });
    }
    return read(url);
  });
  vi.stubGlobal("fetch", fetch);
  const { wrapper, router } = await mountPage("/admin/vendors?q=Preview&state=disabled");
  const card = () => wrapper.get(".vendor-card");
  expect(wrapper.get('.page-heading a[href="/admin/vendors/new"]').text()).toBe("Add vendor");
  expect(wrapper.get('.page-heading a[href="/admin/vendors/new"]').find("svg").exists()).toBe(true);
  expect(card().findAll(".vendor-previews li")).toHaveLength(9);
  expect(card().findAll(".vendor-previews li").at(-1)!.classes()).toContain("add-application");
  expect(card().findAll(".vendor-previews .is-disabled")).toHaveLength(8);
  expect(card().get(".application-preview-name").text()).toBe(apps[0]!.name.en);
  expect(card().get(".vendor-previews a").attributes("aria-label")).toContain(apps[0]!.name.en);
  expect(card().get(".vendor-preview-scroll").attributes("tabindex")).toBe("0");
  expect(card().text()).toContain("8 applications in this view");
  expect(card().text()).toContain("Preview 7");
  expect(card().find(".expanded-apps").exists()).toBe(false);
  expect(card().get('.add-application [aria-label="Add application"]').attributes("href")).toBe("/admin/vendors/openai/apps/new");
  expect(fetch.mock.calls.some(([url]) => new URL(url, "https://test").pathname.endsWith("/apps"))).toBe(true);
  expect(wrapper.find('[aria-label="Vendor pages"]').exists()).toBe(true);
  setLanguage("zh-CN");
  await flushPromises();
  expect(card().get(".app-count").text()).toBe("当前视图 8 个应用");
  setLanguage("en");
  await flushPromises();
  await card().get(".app-count").trigger("click");
  await flushPromises();
  expect(router.currentRoute.value.path).toBe("/admin/vendors/openai/apps");
  expect(router.currentRoute.value.query).toEqual({ q: "Preview", state: "disabled" });
  expect(wrapper.get(".application-table-toolbar input").element).toHaveProperty("value", "Preview");
  expect(wrapper.get(".application-table-toolbar .status-filter [aria-pressed=true]").text()).toBe("Disabled");
  expect(fetch.mock.calls.some(([url]) => url.includes("vendors/openai/apps?q=Preview&state=disabled"))).toBe(true);
  await wrapper.get(".application-table-toolbar input").setValue("");
  await flushPromises();
  await wrapper.findAll(".application-table-toolbar .status-filter button")[0]!.trigger("click");
  await flushPromises();
  expect(router.currentRoute.value.query).toEqual({});
  expect(wrapper.find(".vendor-applications .notice").exists()).toBe(false);
  // Counts remain links for zero and one app; the add tile always stays last.
  previewTotal = 5;
  await router.push("/admin/vendors");
  await flushPromises();
  expect(card().find(".vendor-all-apps").exists()).toBe(false);
  apps = apps.slice(0, 1); previewTotal = 1;
  await wrapper.get('[aria-label="Refresh"]').trigger("click");
  await flushPromises();
  expect(card().text()).toContain("1 application");
  expect(card().text()).not.toContain("1 applications");
  expect(card().findAll(".vendor-previews li")).toHaveLength(2);
  expect(card().find(".add-application").exists()).toBe(true);
  apps = []; previewTotal = 0;
  await wrapper.get('[aria-label="Refresh"]').trigger("click");
  await flushPromises();
  expect(card().findAll(".vendor-previews li")).toHaveLength(1);
  expect(card().get(".app-count").text()).toBe("0 applications");
  expect(card().get(".app-count").attributes("href")).toBe("/admin/vendors/openai/apps");
  expect(card().find(".add-application").exists()).toBe(true);
  wrapper.unmount();
});

it.each(["vendor", "app"] as const)("edits private %s notes with conflict drafts, navigation protection, explicit save and clear", async (kind) => {
  const endpoint = kind === "vendor" ? "/admin/api/vendors/openai/admin-notes" : "/admin/api/apps/openai/codex/admin-notes";
  const path = kind === "vendor" ? "/admin/vendors/openai/admin-notes" : "/admin/vendors/openai/apps/codex/admin-notes";
  let value = { text: "", revision: 0 }, conflict = true;
  const writes: RequestInit[] = [];
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    if (url === endpoint) {
      if (init?.method === "PUT") {
        writes.push(init);
        if (conflict) { conflict = false; value = { text: "Other administrator", revision: 1 }; return response({ error: { code: "DIRECTORY_REVISION_CONFLICT" } }, 409); }
        expect(init.headers).toMatchObject({ "If-Match": `"${value.revision}"`, "X-CSRF-Token": "directory-token" });
        value = { text: JSON.parse(init.body as string).text, revision: value.revision + 1 };
      }
      return response(value);
    }
    return read(url);
  });
  vi.stubGlobal("fetch", fetch);
  const confirm = vi.fn().mockReturnValue(false); vi.stubGlobal("confirm", confirm);
  const { wrapper, router } = await mountPage(path);
  await vi.waitFor(() => expect(wrapper.find('[name="admin-notes"]').exists()).toBe(true));
  const editor = () => wrapper.get(".admin-notes-editor"), input = () => editor().get('[name="admin-notes"]');
  expect(wrapper.get('.application-tabs a[aria-current="page"]').text()).toBe("Admin Notes");
  expect((input().element as HTMLTextAreaElement).value).toBe("");
  await input().setValue("Maintenance <script>private</script>\n  Keep whitespace");
  expect(writes).toHaveLength(0);
  expect(editor().find("script").exists()).toBe(false);
  await editor().get("form").trigger("submit"); await flushPromises();
  expect(editor().get('[role="alert"]').text()).toContain("draft is preserved");
  expect((input().element as HTMLTextAreaElement).value).toContain("Keep whitespace");
  await router.push(path.replace("admin-notes", "settings"));
  expect(router.currentRoute.value.path).toBe(path);
  await editor().get('[aria-label="Reload"]').trigger("click"); await flushPromises();
  expect((input().element as HTMLTextAreaElement).value).toContain("Keep whitespace");
  confirm.mockReturnValue(true);
  await editor().get('[aria-label="Reload"]').trigger("click"); await flushPromises();
  expect((input().element as HTMLTextAreaElement).value).toBe("Other administrator");
  await input().setValue("  Saved private text\n"); await editor().get("form").trigger("submit"); await flushPromises();
  expect(value.text).toBe("  Saved private text\n");
  expect(editor().get('[role="status"]').text()).toBe("Changes saved.");
  await input().setValue(""); await editor().get("form").trigger("submit"); await flushPromises();
  expect(value).toEqual({ text: "", revision: 3 });
  const before = confirm.mock.calls.length;
  await router.push(path.replace("admin-notes", "settings")); await flushPromises();
  expect(confirm.mock.calls.length).toBe(before);
  expect(fetch.mock.calls.filter(([, init]) => init?.method === "PATCH")).toHaveLength(0);
  wrapper.unmount();
});

it.each(["vendor", "app"] as const)("ignores a late %s note save after changing the entity", async (kind) => {
  const source = kind === "vendor" ? "/admin/vendors/openai/admin-notes" : "/admin/vendors/openai/apps/codex/admin-notes";
  const destination = kind === "vendor" ? "/admin/vendors/anthropic/admin-notes" : "/admin/vendors/anthropic/apps/claude-code/admin-notes";
  let finish: ((value: ReturnType<typeof response>) => void) | undefined;
  vi.stubGlobal("confirm", () => true);
  vi.stubGlobal("fetch", vi.fn((url: string, init?: RequestInit) => {
    if (url.endsWith("/admin-notes")) {
      if (init?.method === "PUT") return new Promise<ReturnType<typeof response>>((resolve) => { finish = resolve; });
      return Promise.resolve(response({ text: url.includes("anthropic") ? "Destination private note" : "", revision: 0 }));
    }
    return Promise.resolve(read(url));
  }));
  const { wrapper, router } = await mountPage(source);
  await vi.waitFor(() => expect(wrapper.find('[name="admin-notes"]').exists()).toBe(true));
  await wrapper.get('[name="admin-notes"]').setValue("Source draft");
  await wrapper.get(".admin-notes-editor form").trigger("submit"); await flushPromises();
  await router.push(destination); await flushPromises();
  finish!(response({ text: "Late source saved", revision: 1 })); await flushPromises();
  expect((wrapper.get('[name="admin-notes"]').element as HTMLTextAreaElement).value).toBe("Destination private note");
  expect(wrapper.find(".admin-notes-editor .notice").exists()).toBe(false);
  wrapper.unmount();
});

it.each(["en", "zh-CN"] as const)("keeps detail titles free of provider/state badges and empty vendor logos in %s", async (locale) => {
  setLanguage(locale);
  apps[0]!.provider = "info"; apps[0]!.enabled = false;
  vendors[0]!.icon = ""; vendors[0]!.enabled = false;
  vi.stubGlobal("fetch", vi.fn(async (url: string) => read(url)));
  const { wrapper, router } = await mountPage("/admin/vendors/openai/apps/codex/settings");
  const header = wrapper.get(".application-header");
  expect(header.find(".application-meta").exists()).toBe(false);
  expect(header.find(".state-label").exists()).toBe(false);
  expect(header.text()).not.toContain(locale === "en" ? "App Info" : "应用介绍");
  expect(wrapper.get('.directory-editor [name="enabled"]').attributes("aria-checked")).toBe("false");
  await router.push("/admin/vendors/openai/settings"); await flushPromises();
  expect(wrapper.get(".vendor-header h1").text()).toBe(vendors[0]!.name[locale]);
  expect(wrapper.find(".vendor-header .entity-icon").exists()).toBe(false);
  expect(wrapper.find(".vendor-header .state-label").exists()).toBe(false);
  expect(wrapper.get('.directory-editor [name="enabled"]').attributes("aria-checked")).toBe("false");
  wrapper.unmount();
});

it("saves optional vendor language logos through existing uploads without changing app fields, notes or enabled", async () => {
  vendors[0]!.localized_icons = { en: "", "zh-CN": "" };
  const writes: Record<string, unknown>[] = [];
  let uploadCount = 0;
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    if (url === "/admin/api/assets/icons") {
      expect(init?.headers).toMatchObject({ "X-CSRF-Token": "directory-token" });
      expect(init?.body).toBeInstanceOf(FormData);
      return response({ icon: `/assets/icons/upload-${++uploadCount}.svg` });
    }
    if ((url === "/admin/api/vendors/openai"||url === "/admin/api/vendors/openai/configuration") && init?.method === "PATCH") {
      const body = JSON.parse(init.body as string); writes.push(body);
 if(url.endsWith("/configuration"))return response(applyConfigurationPatch(vendors[0]!,body));
      Object.assign(vendors[0]!, body, { revision: vendors[0]!.revision + 1 });
      return response({ vendor: structuredClone(vendors[0]) });
    }
    return read(url);
  }));
  const { wrapper, router } = await mountPage("/admin/vendors/openai/settings");
  expect(wrapper.get(".vendor-language-icons").attributes("open")).toBeUndefined();
  for (const name of ["icon-en", "icon-zh-CN"]) {
    const upload = wrapper.get(`input[name="${name}"]`);
    Object.defineProperty(upload.element, "files", { value: [new File(["safe svg fixture"], "logo.svg", { type: "image/svg+xml" })] });
    await upload.trigger("change"); await flushPromises();
  }
  await wrapper.get('.directory-editor [name="enabled"]').trigger("click"); await flushPromises();
  expect(writes[0]).not.toHaveProperty("localized_icons");
  await wrapper.get(".directory-editor form").trigger("submit"); await flushPromises();
  expect(writes[1]).toMatchObject({ set: {"localized_icons.en": "/assets/icons/upload-1.svg", "localized_icons.zh-CN": "/assets/icons/upload-2.svg"} });
  expect(writes[1]).not.toHaveProperty("enabled"); expect(writes[1]).not.toHaveProperty("text");
  expect(wrapper.get(".vendor-header img").attributes("src")).toBe("/assets/icons/upload-1.svg");
  setLanguage("zh-CN"); await flushPromises();
  expect(wrapper.get(".vendor-header img").attributes("src")).toBe("/assets/icons/upload-2.svg");
  setLanguage("en"); await flushPromises();
  await wrapper.get('[aria-label="Remove English logo"]').trigger("click");
  await wrapper.get(".directory-editor form").trigger("submit"); await flushPromises();
  expect(vendors[0]!.localized_icons?.en).toBe("");
  await router.push("/admin/vendors/openai/apps/codex/settings"); await flushPromises();
  expect(wrapper.find(".vendor-language-icons").exists()).toBe(false);
  wrapper.unmount();
});

it("requests server-wide table sorting/search, resets paging and renders zero, missing values and bilingual times", async () => {
  const all = Array.from({ length: 23 }, (_, i) => ({ ...structuredClone(apps[0]!), uid: `table-${i}`, id: `item-${i}`, key: `openai/item-${i}`, name: { en: `Item ${i}`, "zh-CN": `项目 ${i}` }, enabled: i % 2 === 0, latest_version: i ? '1.10.0' : '', version_discovered_at: i ? '2026-10-01T00:00:00Z' : null, successful_downloads: i === 1 ? null : i }));
  const calls: URLSearchParams[] = [];
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    const u = new URL(url, 'https://test');
    if (u.pathname === '/admin/api/vendors/openai/apps') {
      calls.push(u.searchParams);
      expect(u.searchParams.get('view')).toBe('table');
      let items = all.filter(a => a.name.en.includes(u.searchParams.get('q') || ''));
      if (u.searchParams.get('sort') === 'downloads') items = [...items].sort((a,b) => (b.successful_downloads ?? -1) - (a.successful_downloads ?? -1));
      const page = Number(u.searchParams.get('page'));
      return response({ items: items.slice((page-1)*20,page*20), page, total: items.length, total_pages: Math.max(1,Math.ceil(items.length/20)) });
    }
    return read(url);
  }));
  const { wrapper, router } = await mountPage('/admin/vendors/openai/apps');
  const table = () => wrapper.get('.application-table');
  expect(table().findAll('tbody tr')).toHaveLength(20);
  expect(table().get('th:first-child').attributes('aria-sort')).toBe('ascending');
  expect(table().get('th:first-child svg').classes()).toContain('lucide-arrow-up');
  expect(table().get('th:nth-child(4) svg').classes()).toContain('lucide-arrow-up-down');
  expect(table().get('tbody tr td.numeric').text()).toBe('0');
  expect(table().findAll('tbody tr')[1]!.findAll('td')[3]!.text()).toBe('—');
  expect(table().get('tbody tr td:nth-child(2)').text()).toBe('—');
  expect(table().get('time').attributes('title')).toBeTruthy();
  expect(wrapper.get('.application-table-toolbar a').text()).toBe('Add application');
  await wrapper.get('[aria-label="Next page"]').trigger('click'); await flushPromises();
  expect(calls.at(-1)!.get('page')).toBe('2');
  await table().get('th:nth-child(4) button').trigger('click'); await flushPromises();
  expect(calls.at(-1)!.get('page')).toBe('1');
  expect(calls.at(-1)!.get('sort')).toBe('downloads');
  expect(table().get('tbody tr').text()).toContain('Item 22');
  await wrapper.get('.application-table-toolbar input').setValue('Item 22'); await flushPromises();
  expect(router.currentRoute.value.query.q).toBe('Item 22');
  expect(calls.at(-1)!.get('q')).toBe('Item 22');
  expect(table().findAll('tbody tr')).toHaveLength(1);
  setLanguage('zh-CN'); await flushPromises();
  expect(calls.at(-1)!.get('lang')).toBe('zh-CN');
  expect(table().text()).toContain('版本发现时间');
  expect(table().text()).toContain('项目 22');
  expect(wrapper.get('.application-table-toolbar a').text()).toBe('添加应用');
  wrapper.unmount();
});

it("mutates table availability with CAS, refreshes filtered last pages, and confirms protected deletion", async () => {
  const base=structuredClone(apps[0]!);
  apps=[{...base,enabled:true,builtin_template:true},...Array.from({length:20},(_,i)=>({...structuredClone(base),uid:`custom-${i}`,id:`custom-${i}`,key:`openai/custom-${i}`,enabled:true,builtin_template:false,name:{en:`Custom ${i}`,'zh-CN':`自定义 ${i}`}}))];
  let conflict=true, failDelete=true, finish: (()=>void)|undefined;
  const writes:{url:string;method:string;body:Record<string,unknown>}[]=[];
  const fetch=vi.fn(async(url:string,init?:RequestInit)=>{
    if(init?.method==='PATCH'||init?.method==='DELETE'){
      const body=JSON.parse(init.body as string), a=apps.find(a=>`/admin/api/apps/${a.key}`===url)!;
      expect(init.headers).toMatchObject({'X-CSRF-Token':'directory-token'});
      writes.push({url,method:init.method,body});
      if(init.method==='PATCH') {
        expect(Object.keys(body).sort()).toEqual(['enabled','revision']);
        return new Promise<ReturnType<typeof response>>(resolve=>{finish=()=>{
          if(conflict){conflict=false;a.revision++;resolve(response({error:{code:'DIRECTORY_REVISION_CONFLICT'}},409));}
          else {expect(body.revision).toBe(a.revision);a.enabled=body.enabled;a.revision++;resolve(response({app:structuredClone(a)}));}
        };});
      }
      expect(body).toEqual({revision:a.revision,confirm_uid:a.uid,confirm_key:a.key});
      if(failDelete){failDelete=false;return response({error:{code:'INTERNAL_ERROR'}},500);}
      apps=apps.filter(x=>x.uid!==a.uid);return response({deleted:true});
    }
    const u=new URL(url,'https://test');
    if(u.pathname==='/admin/api/vendors/openai/apps') {
      const state=u.searchParams.get('state');const items=apps.filter(a=>state==='enabled'?a.enabled:state==='disabled'?!a.enabled:true);
      const pages=Math.max(1,Math.ceil(items.length/20)),page=Math.min(pages,Number(u.searchParams.get('page')));
      return response({items:structuredClone(items.slice((page-1)*20,page*20)),page,total:items.length,total_pages:pages});
    }
    return read(url);
  });
  vi.stubGlobal('fetch',fetch);
  const {wrapper,router}=await mountPage('/admin/vendors/openai/apps?state=enabled');
  const rows=()=>wrapper.findAll('.application-table tbody tr');
  expect(rows()[0]!.get('.danger-link').attributes('disabled')).toBeDefined();
  expect(rows()[0]!.get('.application-row-actions a').attributes('href')).toContain('/settings');
  expect(wrapper.find('.vendor-applications h2').exists()).toBe(false);
  expect(wrapper.get('.table-help button').attributes('aria-describedby')).toBe(wrapper.get('.table-help [role=tooltip]').attributes('id'));
  await wrapper.get('[aria-label="Next page"]').trigger('click');await flushPromises();expect(rows()).toHaveLength(1);
  await rows()[0]!.get('.link-action').trigger('click');await rows()[0]!.get('.link-action').trigger('click');
  expect(writes).toHaveLength(1);expect(rows()[0]!.get('.link-action').attributes('disabled')).toBeDefined();
  finish!();await flushPromises();expect(wrapper.get('[role=alert]').text()).toBeTruthy();expect(rows()[0]!.get('.link-action').text()).toBe('Enabled');
  await rows()[0]!.get('.link-action').trigger('click');finish!();await flushPromises();
  expect(rows()).toHaveLength(20);expect(wrapper.text()).toContain('Page 1 of 1');expect(wrapper.text()).toContain('20 items');
  await wrapper.findAll('.application-table-toolbar .status-filter button')[2]!.trigger('click');await flushPromises();
  expect(router.currentRoute.value.query.state).toBe('disabled');expect(rows()).toHaveLength(1);
  expect(rows()[0]!.find('td:first-child small').exists()).toBe(false);
  setLanguage('zh-CN');await flushPromises();expect(rows()[0]!.get('.link-action').text()).toBe('已禁用');
  await rows()[0]!.get('.danger-link').trigger('click');expect(wrapper.get('.delete-review').text()).toContain('openai/custom-19');
  await wrapper.get('.delete-review .secondary').trigger('click');expect(writes).toHaveLength(2);
  await rows()[0]!.get('.danger-link').trigger('click');await wrapper.get('.delete-review .danger').trigger('click');await flushPromises();
  expect(wrapper.find('.delete-review').exists()).toBe(true);expect(wrapper.find('[role=alert]').exists()).toBe(true);
  await wrapper.get('.delete-review .danger').trigger('click');await flushPromises();
  expect(wrapper.find('.delete-review').exists()).toBe(false);expect(wrapper.text()).toContain('没有匹配的应用');
  wrapper.unmount();
});

it("ignores a late availability result after changing vendors",async()=>{
  let finish: ((result:ReturnType<typeof response>)=>void)|undefined, signal:AbortSignal|undefined;
  const fetch=vi.fn(async(url:string,init?:RequestInit)=>{
    if(init?.method==='PATCH'){signal=init.signal as AbortSignal;return new Promise<ReturnType<typeof response>>(resolve=>{finish=resolve;});}
    const vendor=new URL(url,'https://test').pathname.match(/^\/admin\/api\/vendors\/([^/]+)\/apps$/)?.[1];
    if(vendor){const items=apps.filter(a=>a.vendor_id===vendor);return response({items:structuredClone(items),page:1,total:items.length,total_pages:1});}
    return read(url);
  });vi.stubGlobal('fetch',fetch);
  const {wrapper,router}=await mountPage('/admin/vendors/openai/apps');
  await wrapper.get('.application-row-actions .link-action').trigger('click');
  await router.push('/admin/vendors/anthropic/apps');await flushPromises();
  expect(signal?.aborted).toBe(true);
  const calls=fetch.mock.calls.length;
  finish!(response({app:{...apps[0]!,enabled:!apps[0]!.enabled,revision:90}}));await flushPromises();
  expect(fetch.mock.calls.length).toBe(calls);
  expect(wrapper.get('.vendor-header h1').text()).toBe('Anthropic');
  expect(wrapper.get('.application-table').text()).toContain('Claude Code');
  expect(wrapper.find('[role=alert]').exists()).toBe(false);
  wrapper.unmount();
});

it('does not submit unchanged configuration and sends only the complete proxy leaf for each network mode',async()=>{
 const fetch=vi.fn(async(url:string,init?:RequestInit)=>{
  if(url.endsWith('/configuration')&&init?.method==='PATCH')return response(applyConfigurationPatch(apps[0]!,JSON.parse(init.body as string)));
  return read(url);
 });vi.stubGlobal('fetch',fetch);
 const {wrapper}=await mountPage('/admin/vendors/openai/apps/codex/settings');
 const editor=wrapper.get('.directory-editor');
 await editor.get('form').trigger('submit');await flushPromises();expect(fetch.mock.calls.filter(([,init])=>init?.method==='PATCH')).toHaveLength(0);
 const section=editor.get('.proxy-section');
 for(const mode of ['direct','url','inherit']){
  await selectValue(section,mode);
  if(mode==='url')await section.get('input').setValue('socks5://user:fixture@proxy.example:1080');
  await editor.get('form').trigger('submit');await flushPromises();
  const body=JSON.parse(fetch.mock.calls.filter(([,init])=>init?.method==='PATCH').at(-1)![1]!.body as string);
  expect(Object.keys(body.set)).toEqual(['proxy']);expect(body.unset).toEqual([]);
  expect(body.set.proxy).toEqual(mode==='url'?{mode,url:'socks5://user:fixture@proxy.example:1080'}:{mode});
 }
 wrapper.unmount();
});
