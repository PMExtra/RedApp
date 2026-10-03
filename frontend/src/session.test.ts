import { flushPromises } from "@vue/test-utils";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { api } from "./api";
import { expireSession, loginSession, sessionNotice, signedIn } from "./session";
import { response } from "./testSupport";

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

beforeEach(() => expireSession());
afterEach(() => vi.unstubAllGlobals());

it.each(["headers", "body"])("discards an old 401 delayed at %s after a new login", async (stage) => {
  const oldResponse = deferred<ReturnType<typeof response>>();
  const oldBody = deferred<unknown>();
  const fetch = vi.fn(async (url: string, _init?: RequestInit) => {
    if (url.endsWith("/login")) return response({ csrf: "new-session-token" });
    if (url.endsWith("/old-request")) return stage === "headers"
      ? oldResponse.promise
      : { ...response({}, 401), json: () => oldBody.promise };
    return response({});
  });
  vi.stubGlobal("fetch", fetch);
  // Deliberately reuse the token: ownership is a session generation, not token equality.
  await loginSession("first-login");
  const old = api("old-request").catch((error) => error);
  await flushPromises();
  await loginSession("new-login");
  oldResponse.resolve(response({}, 401));
  oldBody.resolve({});
  const discarded = await old;
  expect(signedIn.value).toBe(true);
  expect(discarded).toMatchObject({ name: "AbortError" });
  expect(sessionNotice.value).toBeUndefined();
  await api("new-request");
  expect((fetch.mock.calls.at(-1)![1]!.headers as Record<string, string>)["X-CSRF-Token"])
    .toBe("new-session-token");
});

it.each([200, 401])("discards a %i response aborted while JSON is pending", async (status) => {
  const body = deferred<unknown>();
  const fetch = vi.fn(async (url: string, _init?: RequestInit) => url.endsWith("/login")
    ? response({ csrf: "current-token" })
    : url.endsWith("/pending-body")
      ? { ...response({}, status), json: () => body.promise }
      : response({}));
  vi.stubGlobal("fetch", fetch);
  await loginSession("fixture");
  const controller = new AbortController();
  const pending = api("pending-body", undefined, controller.signal).catch((error) => error);
  await flushPromises();
  controller.abort();
  body.resolve({ sensitive: "discard me" });
  expect(await pending).toMatchObject({ name: "AbortError" });
  expect(signedIn.value).toBe(true);
  await api("current-request");
  expect((fetch.mock.calls.at(-1)![1]!.headers as Record<string, string>)["X-CSRF-Token"])
    .toBe("current-token");
});
