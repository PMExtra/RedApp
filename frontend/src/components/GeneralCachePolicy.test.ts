import { selectValue } from "../testSupport";
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import GeneralCachePolicy from "./GeneralCachePolicy.vue";
import { resetStores, response } from "../testSupport";
import { signedIn } from "../session";
import { setLanguage } from "../i18n";

const status = {
  running: false,
  interval_seconds: 900,
  scan_limit_per_app: 1000,
  retire_limit_per_app: 100,
  last_attempt_at: null,
  last_success_at: null,
  last_error_at: null,
  last_error: "",
  passes_total: 0,
  failures_total: 0,
  configured_apps: 2,
  scanned_files: 0,
  retired_files: 0,
  skipped_accessed: 0,
  skipped_changed: 0,
  retired_bytes: 0,
};
const policy = {
  revision: 7,
  stale_fallback: true,
  rules: [
    {
      id: "specific",
      match: { type: "glob", pattern: "/releases/" },
      ttl_seconds: 60,
    },
    { id: "catchall", match: { type: "glob", pattern: "/" }, ttl_seconds: 0 },
  ],
  auto_cleanup: [],
};
beforeEach(() => {
  resetStores();
  signedIn.value = true;
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  setLanguage("en");
  signedIn.value = false;
});

it("preserves rule IDs and order, converts cleanup days to seconds, and saves the stale toggle with the app revision", async () => {
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === "PUT")
      return response({ ...JSON.parse(init.body as string), revision: 8 });
    if (url.endsWith("/status")) return response(status);
    return response(structuredClone(policy));
  });
  vi.stubGlobal("fetch", fetch);
  const wrapper = mount(GeneralCachePolicy, {
    props: { application: "acme/files" },
  });
  await flushPromises();
  expect(wrapper.get(".cache-policy").text()).toContain(
    "first matching path sets its TTL",
  );
  expect(wrapper.get(".auto-cleanup-rules").text()).toContain(
    "later rules do not apply",
  );
  expect(wrapper.get(".auto-cleanup-rules").text()).toContain(
    "disabled until rules are added and saved",
  );
  expect(wrapper.find(".auto-cleanup-status").exists()).toBe(false);
  expect(fetch.mock.calls.some(([url]) => url.endsWith("/status"))).toBe(
    false,
  );
  expect(
    wrapper.get('[name="stale_fallback"]').attributes("aria-checked") ===
      "true",
  ).toBe(true);
  const first = wrapper.get(".ttl-rules").findAll(".policy-rule")[0]!;
  await first.get(".sort-handle").trigger("keydown", { key: "ArrowDown" });
  expect(
    (
      wrapper.get(".ttl-rules").findAll('[name="match_pattern"]')[0]!
        .element as HTMLInputElement
    ).value,
  ).toBe("/");
  await wrapper.get('[name="stale_fallback"]').trigger("click");
  await wrapper
    .get(".auto-cleanup-rules")
    .findAll("button")
    .find(
      (button) =>
        button.attributes("aria-label") === "Add automatic cleanup rule",
    )!
    .trigger("click");
  const rule = wrapper.get(".auto-cleanup-rules .policy-rule");
  await selectValue(rule.get('[name="match_type"]'), "re2");
  await rule.get('[name="match_pattern"]').setValue("/releases/.*");
  await selectValue(rule.get('[name="rule_basis"]'), "fetched_at");
  expect(rule.get('[name="cleanup_unit"] [role=combobox]').text()).toBe(
    "Days",
  );
  await rule.get('[name="cleanup_age"]').setValue("7");
  await wrapper
    .get('[aria-label="Add automatic cleanup rule"]')
    .trigger("click");
  const second = wrapper.findAll(".auto-cleanup-rules .policy-rule")[1]!;
  await second.get('[name="match_pattern"]').setValue("/old/");
  await selectValue(second.get('[name="cleanup_unit"]'), "3600");
  await second.get('[name="cleanup_age"]').setValue("2");
  await second.get(".sort-handle").trigger("keydown", { key: "ArrowUp" });
  expect(
    wrapper
      .findAll('.auto-cleanup-rules [name="cleanup_unit"] [role="combobox"]')
      .map((b) => b.text()),
  ).toEqual(["Hours", "Days"]);

  await wrapper.get(".cache-policy form").trigger("submit");
  await flushPromises();
  const request = fetch.mock.calls.find(
    ([, init]) => init?.method === "PUT",
  )!;
  expect(request[0]).toBe("/admin/api/apps/acme/files/cache/policy");
  expect(request[1]!.headers).toMatchObject({ "If-Match": '"7"' });
  const sent = JSON.parse(request[1]!.body as string);
  expect(sent.stale_fallback).toBe(false);
  expect(sent.rules.map((item: { id: string }) => item.id)).toEqual([
    "catchall",
    "specific",
  ]);
  expect(sent.rules[0].ttl_seconds).toBe(0);
  expect(sent.rules[0]).not.toHaveProperty("mode");
  expect(sent.auto_cleanup).toEqual([
    {
      match: { type: "glob", pattern: "/old/" },
      basis: "last_access",
      age_seconds: 7200,
    },
    {
      match: { type: "re2", pattern: "/releases/.*" },
      basis: "fetched_at",
      age_seconds: 604800,
    },
  ]);
  expect(wrapper.get(".cache-policy").text()).toContain("Cache rules saved.");
  wrapper.unmount();
});

it("preserves a CAS conflict draft during status polling and cancels policy loads from the previous application", async () => {
  vi.useFakeTimers();
  let delayed = false,
    resolveOld: ((value: unknown) => void) | undefined;
  const fetch = vi.fn((url: string, init?: RequestInit) => {
    if (init?.method === "PUT")
      return Promise.resolve(
        response({ error: { code: "SETTINGS_REVISION_CONFLICT" } }, 409),
      );
    if (url.endsWith("/status")) return Promise.resolve(response(status));
    if (delayed && url.includes("/acme/files/"))
      return new Promise((resolve) => {
        resolveOld = resolve;
      });
    return Promise.resolve(
      response(
        url.includes("/acme/other/")
          ? {
              revision: 2,
              stale_fallback: false,
              rules: [],
              auto_cleanup: [],
            }
          : structuredClone(policy),
      ),
    );
  });
  vi.stubGlobal("fetch", fetch);
  vi.stubGlobal(
    "confirm",
    vi.fn(() => true),
  );
  const wrapper = mount(GeneralCachePolicy, {
    props: { application: "acme/files" },
  });
  await flushPromises();
  await wrapper.get('[name="rule_ttl"]').setValue("99");
  await wrapper.get(".cache-policy form").trigger("submit");
  await flushPromises();
  expect(wrapper.get(".policy-error").text()).toContain(
    "Your draft is preserved",
  );
  const calls = fetch.mock.calls.length;
  await vi.advanceTimersByTimeAsync(5000);
  await flushPromises();
  expect(fetch.mock.calls.length).toBe(calls);
  expect(wrapper.get(".policy-error").text()).toContain(
    "Your draft is preserved",
  );
  expect(
    (wrapper.get('[name="rule_ttl"]').element as HTMLInputElement).value,
  ).toBe("99");
  delayed = true;
  await wrapper
    .get(".cache-policy")
    .findAll("button")
    .find((button) => button.attributes("aria-label") === "Reload")!
    .trigger("click");
  const oldRequest = fetch.mock.calls.at(-1)![1]!;
  await wrapper.setProps({ application: "acme/other" });
  await flushPromises();
  expect(oldRequest.signal?.aborted).toBe(true);
  resolveOld?.(response(policy));
  await flushPromises();
  expect(wrapper.find('[name="rule_ttl"]').exists()).toBe(false);
  expect(
    wrapper.get('[name="stale_fallback"]').attributes("aria-checked") ===
      "true",
  ).toBe(false);
  expect(wrapper.find(".policy-error").exists()).toBe(false);
  wrapper.unmount();
});
