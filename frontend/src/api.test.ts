import { afterEach, expect, it, vi } from "vitest";
import { api, ApiError, formatDuration, formatMetric } from "./api";
import { errorText, setLanguage } from "./i18n";
import { response } from "./testSupport";
afterEach(() => {
  vi.unstubAllGlobals();
  setLanguage("en");
});
it("formats elapsed durations across zero, unit boundaries and languages without timestamp conversion", () => {
  const cases = [
    [0, "0 s", "0 秒"],
    [59, "59 s", "59 秒"],
    [60, "1 min", "1 分钟"],
    [90, "1.5 min", "1.5 分钟"],
    [3599, "59.98 min", "59.98 分钟"],
    [3600, "1 h", "1 小时"],
    [86399, "1 d", "1 天"],
    [86400, "1 d", "1 天"],
    [129600, "1.5 d", "1.5 天"],
  ] as const;
  for (const [value, en, zh] of cases) {
    setLanguage("en");
    expect(formatDuration(value)).toBe(en);
    expect(formatMetric(value, "seconds")).toBe(en);
    setLanguage("zh-CN");
    expect(formatDuration(value)).toBe(zh);
  }
  for (const value of [null, undefined, -1, NaN, Infinity])
    expect(formatDuration(value)).toBe("—");
});
it("preserves structured problem fields and localizes by stable code before status", async () => {
  for (const [code, status, expected] of [
    ["SETTINGS_REVISION_CONFLICT", 409, "Your draft is preserved"],
    ["CLEANUP_INVALID", 409, "Create a new preview"],
    ["DIRECTORY_DELETE_PENDING", 409, "Retry deletion"],
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

it("distinguishes malformed successful JSON from transport failure", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => ({
      ...response({}),
      json: async () => {
        throw SyntaxError();
      },
    })),
  );
  const error = await api("broken").catch((e) => e);
  expect(error).toMatchObject({ code: "INVALID_RESPONSE", status: 200 });
  expect(errorText(error)).toContain("Invalid server response");
  expect(errorText(new DOMException("cancelled", "AbortError"))).toBe("");
});

it("labels transport failures separately from unexpected application errors", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      throw new TypeError("Failed to fetch");
    }),
  );
  const error = await api("offline").catch((e) => e);
  expect(error).toMatchObject({ code: "NETWORK_ERROR", status: 0 });
  expect(errorText(error)).toContain("Connection failed");
  expect(errorText(new Error("local bug"))).toContain("Unexpected error");
});

it("explains deletion waits in both languages without requesting a reload", () => {
  const error = new ApiError({ code: "DIRECTORY_DELETE_PENDING" }, 409);
  setLanguage("en");
  expect(errorText(error)).toBe(
    "Deletion is not complete. This application is blocked while its tasks stop. Retry deletion; restarting also resumes it.",
  );
  setLanguage("zh-CN");
  expect(errorText(error)).toBe(
    "删除尚未完成。此应用已阻止新任务，正在等待现有任务退出。可重试删除；重启服务也会继续处理。",
  );
});
