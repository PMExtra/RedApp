import { mount, flushPromises } from "@vue/test-utils";
import { createMemoryHistory } from "vue-router";
import App from "./App.vue";
import { makeRouter } from "./router";
import { defaultSite } from "./site";
import {
  bootstrap,
  bootstrapError,
  bootstrapLoading,
  invalidateBootstrap,
  type Application,
} from "./bootstrap";
import {
  cancelSessionCheck,
  sessionBusy,
  sessionChecked,
  sessionError,
  sessionNotice,
  signedIn,
} from "./session";
import { setCSRF } from "./api";
export const applications: Application[] = [
  {
    id: "openai/codex",
    name: { en: "Codex CLI", "zh-CN": "Codex CLI" },
    publisher: "OpenAI",
    summary: { en: "OpenAI coding agent", "zh-CN": "OpenAI 编程助手" },
    icon: "/assets/apps/openai/codex/icon.svg",
    channels: ["latest"],
    installers: [
      { file: "install.sh", shell: "sh" },
      { file: "install.ps1", shell: "powershell" },
    ],
    update_policy: {
      en: "Hashes verified. Provider sign-in is required.",
      "zh-CN": "校验摘要；需要登录服务提供商。",
    },
  },
  {
    id: "anthropic/claude-code",
    name: { en: "Claude Code", "zh-CN": "Claude Code" },
    publisher: "Anthropic",
    summary: { en: "Anthropic coding agent", "zh-CN": "Anthropic 编程助手" },
    icon: "",
    channels: ["latest", "stable"],
    installers: [
      { file: "install.sh", shell: "bash" },
      { file: "install.ps1", shell: "powershell" },
    ],
    update_policy: {
      en: "Signed manifest verified. Managed launcher disables official updates.",
      "zh-CN": "验证签名清单，受管理的启动入口会禁用官方更新。",
    },
  },
];
export const boot = {
  version: "dev",
  os: "linux",
  arch: "arm64",
  site: structuredClone(defaultSite),
  apps: applications,
  public_origin: "https://downloads.example:8443",
  revision: "1",
};
export const status = {
  name: "RedApp",
  sampled_at: "2026-10-02T08:01:00Z",
  public_base_url: boot.public_origin,
  os: "linux",
  arch: "amd64",
  go: "go1.27.0",
  goroutines: 4,
  memory_bytes: 1024,
  client_runtime_update_policy: "",
  disk: {
    cache_bytes: 0,
    temporary_bytes: 0,
    pending_bytes: 0,
    used_bytes: 1024,
    free_bytes: 10000,
  },
  rates: { upstream_bytes_per_second: 0, downstream_bytes_per_second: 0 },
  counters: {},
  metrics: [],
};
export const response = (data: unknown, status = 200) => ({
  ok: status >= 200 && status < 300,
  status,
  json: async () => data,
});
export function resetStores() {
  cancelSessionCheck();
  setCSRF("");
  invalidateBootstrap();
  bootstrap.value = undefined;
  bootstrapError.value = undefined;
  bootstrapLoading.value = false;
  signedIn.value = false;
  sessionChecked.value = false;
  sessionBusy.value = false;
  sessionError.value = undefined;
  sessionNotice.value = undefined;
}
export async function mountPage(path: string) {
  const router = makeRouter(createMemoryHistory());
  await router.push(path);
  await router.isReady();
  const wrapper = mount(App, {
    global: { plugins: [router] },
    attachTo: document.body,
  });
  await flushPromises();
  await flushPromises();
  return { wrapper, router };
}
