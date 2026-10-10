import { http, HttpResponse } from "msw";
import { afterEach, describe, expect, it, vi } from "vitest";
import { apiError, useHandlers } from "@/test/msw";
import { uploadWithProgress } from "./upload";

afterEach(() => {
  vi.restoreAllMocks();
});

/** The abort listeners an upload leaves on its signal once it has settled. */
async function leftoverListeners(respond: () => Response) {
  useHandlers(http.post("*/admin/api/upload-test", respond));
  const signal = new AbortController().signal;
  const added = vi.spyOn(signal, "addEventListener");
  const removed = vi.spyOn(signal, "removeEventListener");
  await uploadWithProgress({ url: "/admin/api/upload-test", body: new Blob(["x"]), signal }).catch(
    (error: unknown) => error,
  );
  const listeners = (spy: typeof added) =>
    spy.mock.calls.filter(([type]) => type === "abort").map(([, listener]) => listener);
  const removedListeners = listeners(removed);
  return listeners(added).filter((listener) => !removedListeners.includes(listener));
}

describe("uploadWithProgress", () => {
  it.each([
    ["a success", () => HttpResponse.json({ ok: true })],
    ["an error response", () => apiError("INTERNAL_ERROR")],
    ["a network failure", () => HttpResponse.error()],
  ])("releases the abort listener after %s", async (_name, respond) => {
    expect(await leftoverListeners(respond)).toEqual([]);
  });
});
