import { flushPromises } from "@vue/test-utils";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { boot, mountPage, resetStores, response } from "../testSupport";
beforeEach(resetStores);
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  Object.defineProperty(document, "visibilityState", {
    value: "visible",
    configurable: true,
  });
  document.body.innerHTML = "";
});
it("loads independent event pages, pauses hidden polling, and clears private data on 401", async () => {
  vi.useFakeTimers();
  let expired = false;
  const fetch = vi.fn(async (url: string) =>
    url === "/api/bootstrap"
      ? response(boot)
      : url.endsWith("/session")
        ? response({ csrf: "token" })
        : expired
          ? response(
              { error: { code: "AUTH_REQUIRED", message: "Sign in" } },
              401,
            )
          : response({ items: [], next_cursor: null }),
  );
  vi.stubGlobal("fetch", fetch);
  const { wrapper } = await mountPage("/admin/events");
  expect(wrapper.text()).toContain("No recent failures");
  expect(fetch.mock.calls.some(([url]) => url.endsWith("/status"))).toBe(false);
  expect(
    fetch.mock.calls.some(([url]) => url === "/admin/api/events?limit=50"),
  ).toBe(true);
  await vi.advanceTimersByTimeAsync(5000);
  expect(
    fetch.mock.calls.filter(([url]) => url.includes("/events?")),
  ).toHaveLength(2);
  Object.defineProperty(document, "visibilityState", {
    value: "hidden",
    configurable: true,
  });
  document.dispatchEvent(new Event("visibilitychange"));
  const calls = fetch.mock.calls.length;
  await vi.advanceTimersByTimeAsync(10000);
  expect(fetch.mock.calls.length).toBe(calls);
  expired = true;
  Object.defineProperty(document, "visibilityState", {
    value: "visible",
    configurable: true,
  });
  document.dispatchEvent(new Event("visibilitychange"));
  await flushPromises();
  await vi.waitFor(() => expect(wrapper.find(".login").exists()).toBe(true));
  expect(wrapper.text()).not.toContain("No recent failures");
  const ended = fetch.mock.calls.length;
  await vi.advanceTimersByTimeAsync(10000);
  expect(fetch.mock.calls.length).toBe(ended);
  wrapper.unmount();
});
