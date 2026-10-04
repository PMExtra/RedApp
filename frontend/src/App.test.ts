import { flushPromises } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setLanguage } from "./i18n";
import { boot, mountPage, resetStores, response, status } from "./testSupport";
import { defaultSite } from "./site";
import { api } from "./api";
beforeEach(resetStores);
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  setLanguage("en");
  document.body.innerHTML = "";
});
describe("SPA page ownership", () => {
  it("retains snapshots on error, polls only visible active pages, and expires sessions", async () => {
    vi.useFakeTimers();
    let phase = "ready";
    const fetch = vi.fn(async (url: string, _init?: RequestInit) =>
      url === "/api/bootstrap"
        ? response(boot)
        : url.endsWith("/session")
          ? response({ csrf: "token" })
          : phase === "expired"
            ? response({}, 401)
            : phase === "error"
              ? response({}, 503)
              : response(status),
    );
    vi.stubGlobal("fetch", fetch);
    const { wrapper } = await mountPage("/admin/overview");
    expect(wrapper.text()).toContain("Distribution overview");
    phase = "error";
    await vi.advanceTimersByTimeAsync(5000);
    await flushPromises();
    expect(wrapper.text()).toContain("Showing the last successful snapshot");
    Object.defineProperty(document, "visibilityState", {
      value: "hidden",
      configurable: true,
    });
    document.dispatchEvent(new Event("visibilitychange"));
    const count = fetch.mock.calls.length;
    await vi.advanceTimersByTimeAsync(10000);
    expect(fetch.mock.calls.length).toBe(count);
    Object.defineProperty(document, "visibilityState", {
      value: "visible",
      configurable: true,
    });
    phase = "expired";
    document.dispatchEvent(new Event("visibilitychange"));
    await flushPromises();
    await flushPromises();
    expect(wrapper.text()).toContain("Your session expired");
    await vi.waitFor(() => expect(wrapper.find(".login").exists()).toBe(true));
    expect(wrapper.text()).not.toContain("Distribution overview");
    expect(wrapper.find(".account-trigger").exists()).toBe(false);
    await expect(api("after-expiry")).rejects.toMatchObject({ status: 401 });
    expect(
      (fetch.mock.calls.at(-1)![1]!.headers as Record<string, string>)[
        "X-CSRF-Token"
      ],
    ).toBe("");
    const ended = fetch.mock.calls.length;
    await vi.advanceTimersByTimeAsync(10000);
    expect(fetch.mock.calls.length).toBe(ended);
    wrapper.unmount();
  });
  it("opens settings without a status request, preserves a conflict draft, and guards navigation", async () => {
    vi.useFakeTimers();
    const fetch = vi.fn(async (url: string, init?: RequestInit) =>
      url === "/api/bootstrap"
        ? response(boot)
        : url.endsWith("/session")
          ? response({ csrf: "token" })
          : url.endsWith("public-url")
            ? response({
                override_url: null,
                environment_url: null,
                effective_url: boot.public_origin,
                source: "request",
                revision: 0,
              })
            : init?.method === "PUT"
              ? response(
                  {
                    error: {
                      code: "SETTINGS_REVISION_CONFLICT",
                      message: "Settings changed",
                    },
                  },
                  409,
                )
              : response({ ...defaultSite, revision: 0 }),
    );
    vi.stubGlobal("fetch", fetch);
    const confirm = vi.fn().mockReturnValue(false);
    vi.stubGlobal("confirm", confirm);
    const { wrapper, router } = await mountPage("/admin/settings/site");
    await wrapper.find(".site-settings input").setValue("Unsaved draft");
    await wrapper.find(".site-settings form").trigger("submit");
    await flushPromises();
    expect(wrapper.text()).toContain("Your draft is preserved");
    await vi.advanceTimersByTimeAsync(10000);
    expect(fetch.mock.calls.some(([url]) => url.endsWith("/status"))).toBe(
      false,
    );
    expect(
      (wrapper.find(".site-settings input").element as HTMLInputElement).value,
    ).toBe("Unsaved draft");
    await wrapper.find(".account-trigger").trigger("click");
    await wrapper
      .findAll("[role=menuitem]")
      .find((item) => item.text() === "Sign out")!
      .trigger("click");
    expect(fetch.mock.calls.some(([url]) => url.endsWith("/logout"))).toBe(
      false,
    );
    await router.push("/admin/overview");
    expect(router.currentRoute.value.path).toBe("/admin/settings/site");
    expect(confirm).toHaveBeenCalled();
    wrapper.unmount();
  });
  it("logs in and signs out with CSRF, discarding a late status result", async () => {
    let resolveStatus: ((value: unknown) => void) | undefined;
    const fetch = vi.fn((url: string, init?: RequestInit) =>
      url === "/api/bootstrap"
        ? Promise.resolve(response(boot))
        : url.endsWith("/session")
          ? Promise.resolve(response({}, 401))
          : url.endsWith("/login")
            ? Promise.resolve(response({ csrf: "login-token" }))
            : url.endsWith("/logout")
              ? Promise.resolve(response({ ok: true }))
              : new Promise((r) => {
                  resolveStatus = r;
                }),
    );
    vi.stubGlobal("fetch", fetch);
    const { wrapper } = await mountPage("/admin/login");
    await wrapper.find("input[type=password]").setValue("fixture");
    await wrapper.find("form").trigger("submit");
    await flushPromises();
    await flushPromises();
    await wrapper.find(".account-trigger").trigger("click");
    await wrapper
      .findAll("[role=menuitem]")
      .find((x) => x.text() === "Sign out")!
      .trigger("click");
    await flushPromises();
    resolveStatus?.(response(status));
    await flushPromises();
    await flushPromises();
    await vi.waitFor(() => expect(wrapper.find(".login").exists()).toBe(true));
    expect(wrapper.text()).not.toContain("Distribution overview");
    expect(
      (
        fetch.mock.calls.find(([url]) => url.endsWith("/logout"))![1]!
          .headers as Record<string, string>
      )["X-CSRF-Token"],
    ).toBe("login-token");
    wrapper.unmount();
  });
});

it("redirects every signed-out admin entry after abandoning login, including history and deep links", async () => {
  let authenticated = false;
  const fetch = vi.fn(async (url: string) => {
    if (url === "/api/bootstrap") return response(boot);
    if (url.endsWith("/session"))
      return authenticated ? response({ csrf: "token" }) : response({}, 401);
    if (url.endsWith("/login")) {
      authenticated = true;
      return response({ csrf: "token" });
    }
    if (url.endsWith("/status")) return response(status);
    return response({ items: [], total: 0 });
  });
  vi.stubGlobal("fetch", fetch);
  const { wrapper, router } = await mountPage("/");
  const assertLogin = async (target: string) => {
    await vi.waitFor(() =>
      expect(router.currentRoute.value.path).toBe("/admin/login"),
    );
    await vi.waitFor(() =>
      expect(wrapper.find(".login input[type=password]").exists()).toBe(true),
    );
    expect(router.currentRoute.value.query.returnTo).toBe(target);
    expect(wrapper.text()).not.toContain("Distribution overview");
  };
  for (let visit = 0; visit < 2; visit++) {
    await wrapper.get('a[href="/admin/overview"]').trigger("click");
    await assertLogin("/admin/overview");
    await wrapper.get('.topbar a[href="/"]').trigger("click");
    await vi.waitFor(() => expect(router.currentRoute.value.path).toBe("/"));
  }
  router.back();
  await assertLogin("/admin/overview");
  router.forward();
  await vi.waitFor(() => expect(router.currentRoute.value.path).toBe("/"));
  await router.push("/admin/events?severity=error");
  await assertLogin("/admin/events?severity=error");
  await wrapper.get("input[type=password]").setValue("fixture");
  await wrapper.get(".login form").trigger("submit");
  await vi.waitFor(() =>
    expect(router.currentRoute.value.fullPath).toBe(
      "/admin/events?severity=error",
    ),
  );
  expect(wrapper.find(".login").exists()).toBe(false);
  await router.push("/");
  await router.push("/admin/overview");
  await vi.waitFor(() =>
    expect(wrapper.text()).toContain("Distribution overview"),
  );
  wrapper.unmount();
});
