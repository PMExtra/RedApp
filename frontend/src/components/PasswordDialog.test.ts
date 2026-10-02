import { mount, flushPromises } from "@vue/test-utils";
import { afterEach, it, expect, vi } from "vitest";
import PasswordDialog from "./PasswordDialog.vue";
import { setLanguage } from "../i18n";
import { setCSRF } from "../api";
afterEach(() => {
  vi.unstubAllGlobals();
  setLanguage("en");
});
it("validates confirmation, localizes failure and submits once while pending", async () => {
  setLanguage("zh-CN");
  setCSRF("test-csrf");
  let release: ((value: unknown) => void) | undefined;
  const fetch = vi.fn(
    (_url: string, _options?: RequestInit) =>
      new Promise((resolve) => {
        release = resolve;
      }),
  );
  vi.stubGlobal("fetch", fetch);
  const trigger = document.createElement("button");
  document.body.append(trigger);
  trigger.focus();
  const wrapper = mount(PasswordDialog, { attachTo: document.body });
  const fields = wrapper.findAll("input");
  await fields[0].setValue("old-fixture");
  await fields[1].setValue("new-long-fixture");
  await fields[2].setValue("different");
  await wrapper.find("form").trigger("submit");
  expect(wrapper.text()).toContain("两次输入的新密码不一致");
  expect(fetch).not.toHaveBeenCalled();
  await fields[2].setValue("new-long-fixture");
  await wrapper.find("form").trigger("submit");
  await wrapper.find("form").trigger("submit");
  await wrapper.find("dialog").trigger("cancel");
  expect(wrapper.emitted("close")).toBeUndefined();
  expect(fetch).toHaveBeenCalledTimes(1);
  expect(
    (fetch.mock.calls[0][1]!.headers as Record<string, string>)["X-CSRF-Token"],
  ).toBe("test-csrf");
  release?.({
    ok: false,
    status: 400,
    json: async () => ({ error: "Wrong password" }),
  });
  await flushPromises();
  expect(wrapper.text()).toContain("请求被拒绝");
  expect(wrapper.emitted("changed")).toBeUndefined();
  await wrapper.find("form").trigger("submit");
  release?.({ ok: true, status: 200, json: async () => ({ ok: true }) });
  await flushPromises();
  expect(wrapper.emitted("changed")).toHaveLength(1);
  expect(fields.map((f) => (f.element as HTMLInputElement).value)).toEqual([
    "",
    "",
    "",
  ]);
  wrapper.unmount();
  expect(document.activeElement).toBe(trigger);
  trigger.remove();
});
it("cancels before submission and discards pending work when unmounted", async () => {
  let aborted = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(
      (_url: string, options?: RequestInit) =>
        new Promise((_resolve, reject) =>
          options?.signal?.addEventListener("abort", () => {
            aborted = true;
            reject(new DOMException("cancel", "AbortError"));
          }),
        ),
    ),
  );
  const wrapper = mount(PasswordDialog);
  await wrapper.find("dialog").trigger("cancel");
  expect(wrapper.emitted("close")).toHaveLength(1);
  const fields = wrapper.findAll("input");
  await fields[0].setValue("old-fixture");
  await fields[1].setValue("new-long-fixture");
  await fields[2].setValue("new-long-fixture");
  await wrapper.find("form").trigger("submit");
  wrapper.unmount();
  await flushPromises();
  expect(aborted).toBe(true);
  expect(wrapper.emitted("changed")).toBeUndefined();
});
