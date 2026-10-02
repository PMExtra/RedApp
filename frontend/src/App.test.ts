import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, describe, it, expect, vi } from "vitest";
import { localDate, setLanguage } from "./i18n";
import App from "./App.vue";
import Maintenance from "./components/Maintenance.vue";
const status = {
  name: "RedApp",
  public_base_url: "https://redapp.example",
  sampled_at: "2026-10-02T08:01:00Z",
  os: "linux",
  arch: "amd64",
  go: "go1.27.1",
  goroutines: 4,
  memory_bytes: 1024,
  disk: {
    cache_bytes: 0,
    temporary_bytes: 0,
    pending_bytes: 0,
    used_bytes: 1024,
    free_bytes: 10000,
  },
  rates: { upstream_bytes_per_second: 0, downstream_bytes_per_second: 0 },
  resources: [],
  versions: {},
  events: [],
  counters: {},
  metrics: [],
};
const info = { version: "0.4.0", os: "linux", arch: "amd64" };
const response = (data: unknown, status = 200) => ({
  ok: status === 200,
  status,
  json: async () => data,
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  setLanguage("en");
});
describe("Admin session and requests", () => {
  it("retains last snapshot on failure, localizes it, expires sessions and stops polling", async () => {
    vi.useFakeTimers();
    let phase = "ready";
    const fetch = vi.fn(async (url: string) =>
      url === "/api/info"
        ? response(info)
        : url.endsWith("/session")
          ? response({ csrf: "test-token" })
          : phase === "error"
            ? response({ error: "Storage unavailable" }, 503)
            : phase === "expired"
              ? response({ error: "Sign in required" }, 401)
              : response(status),
    );
    vi.stubGlobal("fetch", fetch);
    const wrapper = mount(App);
    await flushPromises();
    expect(wrapper.text()).toContain("Distribution overview");
    expect(wrapper.find("time").text()).toBe(localDate(status.sampled_at));
    expect(wrapper.find("input[type=checkbox]").exists()).toBe(false);
    phase = "error";
    await wrapper
      .findAll("button")
      .find((b) => b.text() === "Refresh")!
      .trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("Service temporarily unavailable");
    expect(wrapper.text()).toContain("Showing the last successful snapshot");
    await wrapper.find(".language-control select").setValue("zh-CN");
    expect(wrapper.text()).toContain("服务暂时不可用");
    expect(wrapper.find("time").text()).toBe(localDate(status.sampled_at));
    phase = "ready";
    await wrapper.find("[role=alert] button").trigger("click");
    await flushPromises();
    expect(wrapper.find("[role=alert]").exists()).toBe(false);
    phase = "expired";
    await vi.advanceTimersByTimeAsync(5000);
    await flushPromises();
    expect(wrapper.text()).toContain("会话已过期");
    const calls = fetch.mock.calls.length;
    await vi.advanceTimersByTimeAsync(10000);
    expect(fetch.mock.calls.length).toBe(calls);
    wrapper.unmount();
  });
  it("logs in with CSRF and signs out using the account menu", async () => {
    const fetch = vi.fn(async (url: string, _options?: RequestInit) =>
      url === "/api/info"
        ? response(info)
        : url.endsWith("/session")
          ? response({}, 401)
          : url.endsWith("/login")
            ? response({ csrf: "login-token" })
            : url.endsWith("/logout")
              ? response({ ok: true })
              : response(status),
    );
    vi.stubGlobal("fetch", fetch);
    const wrapper = mount(App);
    await flushPromises();
    await wrapper.find("input[type=password]").setValue("dummy-password");
    await wrapper.find("form").trigger("submit");
    await flushPromises();
    expect(wrapper.text()).toContain("Distribution overview");
    await wrapper.find(".account-trigger").trigger("click");
    await wrapper
      .findAll("[role=menuitem]")
      .find((b) => b.text() === "Sign out")!
      .trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("Signed out");
    const options = fetch.mock.calls.find((call) =>
      call[0].endsWith("/logout"),
    )?.[1] as RequestInit;
    expect((options.headers as Record<string, string>)["X-CSRF-Token"]).toBe(
      "login-token",
    );
    wrapper.unmount();
  });
  it("toggles polling accessibly and does not overlap refreshes or restore data after sign-out", async () => {
    vi.useFakeTimers();
    let release: ((value: unknown) => void) | undefined;
    let pending = false;
    const fetch = vi.fn((url: string) =>
      url === "/api/info"
        ? Promise.resolve(response(info))
        : url.endsWith("/session")
          ? Promise.resolve(response({ csrf: "token" }))
          : url.endsWith("/logout")
            ? Promise.resolve(response({ ok: true }))
            : pending
              ? new Promise((resolve) => {
                  release = resolve;
                })
              : Promise.resolve(response(status)),
    );
    vi.stubGlobal("fetch", fetch);
    const wrapper = mount(App);
    await flushPromises();
    await wrapper.find(".auto-refresh").trigger("click");
    expect(wrapper.find(".auto-refresh").attributes("aria-pressed")).toBe(
      "false",
    );
    const before = fetch.mock.calls.length;
    await vi.advanceTimersByTimeAsync(10000);
    expect(fetch.mock.calls.length).toBe(before);
    await wrapper.find(".auto-refresh").trigger("click");
    pending = true;
    await vi.advanceTimersByTimeAsync(5000);
    await flushPromises();
    const after = fetch.mock.calls.length;
    await vi.advanceTimersByTimeAsync(15000);
    expect(fetch.mock.calls.length).toBe(after);
    await wrapper.find(".account-trigger").trigger("click");
    await wrapper
      .findAll("[role=menuitem]")
      .find((b) => b.text() === "Sign out")!
      .trigger("click");
    await flushPromises();
    release?.(response(status));
    await flushPromises();
    expect(wrapper.find(".login").exists()).toBe(true);
    expect(wrapper.text()).not.toContain("Distribution overview");
    wrapper.unmount();
  });
  it("requires cleanup preview, discards an edited pending preview and prevents duplicate execution", async () => {
    let resolvePreview: ((value: unknown) => void) | undefined,
      resolveExecute: ((value: unknown) => void) | undefined;
    const fetch = vi.fn((url: string, _options?: RequestInit) =>
      url.endsWith("/settings")
        ? Promise.resolve(response({ latest_ttl_seconds: 120 }))
        : url.endsWith("/preview")
          ? new Promise((resolve) => {
              resolvePreview = resolve;
            })
          : new Promise((resolve) => {
              resolveExecute = resolve;
            }),
    );
    vi.stubGlobal("fetch", fetch);
    const wrapper = mount(Maintenance);
    await flushPromises();
    expect(
      (wrapper.find("input[type=number]").element as HTMLInputElement).value,
    ).toBe("120");
    const input = wrapper.find('input[placeholder="0.150.0"]');
    await input.setValue("0.159.2");
    await wrapper.find("form.cleanup").trigger("submit");
    await input.setValue("0.160.0");
    resolvePreview?.(
      response({
        job: { ID: "old", Selected: [] },
        logical_bytes: 0,
        active: 0,
      }),
    );
    await flushPromises();
    expect(wrapper.find(".cleanup-review").exists()).toBe(false);
    await wrapper.find("form.cleanup").trigger("submit");
    resolvePreview?.(
      response({
        job: { ID: "new", Selected: [] },
        logical_bytes: 0,
        active: 0,
      }),
    );
    await flushPromises();
    const confirm = wrapper.find(".cleanup-review .danger");
    await confirm.trigger("click");
    await confirm.trigger("click");
    expect(
      fetch.mock.calls.filter((call) => call[0].endsWith("/execute")),
    ).toHaveLength(1);
    resolveExecute?.(response({ ok: true }));
    await flushPromises();
    expect(wrapper.find(".cleanup-review").exists()).toBe(false);
    wrapper.unmount();
  });
});
