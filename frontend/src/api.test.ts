import { afterEach, expect, it, vi } from "vitest";
import { api, ApiError } from "./api";
import { errorText, setLanguage } from "./i18n";
import { response } from "./testSupport";
afterEach(() => {
  vi.unstubAllGlobals();
  setLanguage("en");
});
it("preserves structured problem fields and localizes by stable code before status", async () => {
  for (const [code, status, expected] of [
    ["SETTINGS_REVISION_CONFLICT", 409, "Your draft is preserved"],
    ["CLEANUP_INVALID", 409, "Create a new preview"],
    ["METADATA_UNTRUSTED", 502, "could not be verified"],
  ] as const) {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        response(
          {
            error: {
              code,
              message: "Server detail",
              request_id: "request-123",
              retryable: false,
            },
          },
          status,
        ),
      ),
    );
    const error = await api("fixture").catch((reason) => reason);
    expect(error).toBeInstanceOf(ApiError);
    if (!(error instanceof ApiError)) throw error;
    expect(error.message).toBe("Server detail");
    expect(error.code).toBe(code);
    expect(error.request_id).toBe("request-123");
    expect(error.retryable).toBe(false);
    expect(errorText(error)).toContain(expected);
    setLanguage("zh-CN");
    expect(errorText(error)).not.toContain(expected);
    setLanguage("en");
  }
  expect(errorText(new ApiError("legacy conflict", 409))).not.toContain(
    "Settings",
  );
  expect(errorText(new ApiError({ code: "FUTURE_ERROR" }, 503))).toContain(
    "Service temporarily unavailable",
  );
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => response({ error: "legacy message" }, 404)),
  );
  const legacy = await api("fixture").catch((reason) => reason);
  if (!(legacy instanceof ApiError)) throw legacy;
  expect(legacy.message).toBe("legacy message");
  expect(errorText(legacy)).toContain("Requested data is unavailable");
});
