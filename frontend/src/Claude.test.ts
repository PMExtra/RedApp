import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, expect, it, vi } from "vitest";
import PublicApp from "./PublicApp.vue";
import Resources from "./components/Resources.vue";
import Maintenance from "./components/Maintenance.vue";
import { setLanguage } from "./i18n";
import type { Status } from "./api";

afterEach(() => {
  vi.unstubAllGlobals();
  window.history.replaceState({}, "", "/");
  setLanguage("en");
});

it("renders Claude commands and update boundary in both languages without Codex branding", async () => {
  window.history.replaceState({}, "", "/apps/claude-code");
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => ({
      ok: true,
      json: async () =>
        url === "/api/info"
          ? { version: "dev", os: "linux", arch: "amd64" }
          : [
              {
                id: "codex",
                name: "Codex CLI",
                origin: "https://internal.example",
                icon: "/apps/codex/icon.svg",
              },
              {
                id: "claude-code",
                name: "Claude Code",
                origin: "https://internal.example",
                icon: "",
              },
            ],
    })),
  );
  const wrapper = mount(PublicApp);
  await flushPromises();
  expect(wrapper.find("h1").text()).toBe("Claude Code");
  expect(wrapper.find(".application-identity img").exists()).toBe(false);
  const commands = [
    "curl -fsSL 'https://internal.example/claude-code/install.sh' | bash",
    "irm 'https://internal.example/claude-code/install.ps1' | iex",
  ];
  expect(wrapper.findAll(".command code").map((x) => x.text())).toEqual(
    commands,
  );
  expect(wrapper.text()).toContain(
    "managed launcher disables official updates",
  );
  expect(wrapper.text()).not.toContain("CODEX_RELEASE");
  setLanguage("zh-CN");
  await flushPromises();
  expect(wrapper.text()).toContain("受管理的启动入口会禁用官方更新");
  expect(wrapper.text()).toContain("直接运行版本二进制会绕过更新控制");
  expect(wrapper.findAll(".command code").map((x) => x.text())).toEqual(
    commands,
  );
  wrapper.unmount();
});

it("separates same-version histories, resources and counters by application", async () => {
  const data = {
    versions: { "2.1.285": "2026-01-01" },
    application_versions: {
      codex: { "2.1.285": "2026-01-01" },
      "claude-code": { "2.1.285": "2026-02-01" },
    },
    counters: {
      "version:2.1.285:requests": 3,
      "app:claude-code:version:2.1.285:requests": 7,
    },
    resources: [
      {
        ID: "codex",
        State: "complete",
        Resource: {
          Labels: { app: "codex", version: "2.1.285", name: "codex-file" },
        },
        VerificationNS: 0,
      },
      {
        ID: "claude",
        State: "complete",
        Resource: {
          Labels: {
            app: "claude-code",
            version: "2.1.285",
            name: "claude-file",
          },
        },
        VerificationNS: 0,
      },
    ],
  } as unknown as Status;
  const wrapper = mount(Resources, { props: { status: data } });
  expect(wrapper.text()).toContain("codex-file");
  expect(wrapper.text()).not.toContain("claude-file");
  await wrapper.findAll("[role=combobox]")[0]!.trigger("click");
  await wrapper
    .findAll("[role=option]")
    .find((x) => x.text() === "Claude Code")!
    .trigger("click");
  expect(wrapper.text()).toContain("claude-file");
  expect(wrapper.text()).not.toContain("codex-file");
  expect(wrapper.text()).toContain("7 artifact requests");
  wrapper.unmount();
});

it("scopes TTL and cleanup preview to the selected app and clears stale preview", async () => {
  const fetch = vi.fn(async () => ({
    ok: true,
    json: async () => ({
      latest_ttl_seconds: 60,
      job: { ID: "snapshot", Selected: [] },
      unknown_versions: [],
    }),
  }));
  vi.stubGlobal("fetch", fetch);
  const wrapper = mount(Maintenance);
  await flushPromises();
  await wrapper.find("[role=combobox]").trigger("click");
  await wrapper
    .findAll("[role=option]")
    .find((x) => x.text() === "Claude Code")!
    .trigger("click");
  await flushPromises();
  await wrapper.find(".cleanup input").setValue("2.1.285");
  await wrapper.find(".cleanup").trigger("submit");
  await flushPromises();
  const call = fetch.mock.calls.at(-1) as unknown as [string, RequestInit];
  expect(call[0]).toContain("cleanup/preview");
  expect(
    (call[1].headers as Record<string, string>)["X-RedApp-Application"],
  ).toBe("claude-code");
  expect(wrapper.find(".cleanup-review").exists()).toBe(true);
  await wrapper.find("[role=combobox]").trigger("click");
  await wrapper
    .findAll("[role=option]")
    .find((x) => x.text() === "Codex CLI")!
    .trigger("click");
  await flushPromises();
  expect(wrapper.find(".cleanup-review").exists()).toBe(false);
  wrapper.unmount();
});
