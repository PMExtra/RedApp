import { describe, expect, it, vi } from "vitest";
import { http, HttpResponse } from "msw";
import { apiError, mockApi, noContent, useHandlers } from "@/test/msw";
import { bootstrap, session, siteSettingsState } from "@/test/factories";
import { api, configureApi, unwrap } from "./client";
import { ApiError } from "./errors";
import { ifMatch, ifMatchHeader, revisionFromEtag } from "./revision";

describe("unwrap", () => {
  it("resolves with the response body", async () => {
    useHandlers(mockApi("get", "/api/bootstrap", () => bootstrap({ version: "1.2.3" })));
    const data = await unwrap(api.GET("/api/bootstrap"));
    expect(data.version).toBe("1.2.3");
  });

  it("rejects with the spec error fields", async () => {
    useHandlers(
      mockApi("get", "/api/apps/{vendor}/{app}", () =>
        apiError("APPLICATION_NOT_FOUND", {
          message: "Application not found",
          requestId: "00112233aabbccdd",
        }),
      ),
    );
    const error = await unwrap(
      api.GET("/api/apps/{vendor}/{app}", { params: { path: { vendor: "a", app: "b" } } }),
    ).catch((reason: unknown) => reason);
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({
      code: "APPLICATION_NOT_FOUND",
      message: "Application not found",
      requestId: "00112233aabbccdd",
      retryable: false,
      status: 404,
    });
  });

  it("keeps the server's retryable flag", async () => {
    useHandlers(mockApi("get", "/api/bootstrap", () => apiError("STORAGE_UNAVAILABLE")));
    const error = await unwrap(api.GET("/api/bootstrap")).catch((reason: unknown) => reason);
    expect(error).toMatchObject({ code: "STORAGE_UNAVAILABLE", retryable: true, status: 503 });
  });

  it("maps a non-spec error body to UNEXPECTED_RESPONSE with the header request ID", async () => {
    useHandlers(
      http.get("*/api/bootstrap", () =>
        HttpResponse.text("<html>Bad gateway</html>", {
          status: 502,
          headers: { "X-Request-Id": "aaaabbbbccccdddd" },
        }),
      ),
    );
    const error = await unwrap(api.GET("/api/bootstrap")).catch((reason: unknown) => reason);
    expect(error).toMatchObject({
      code: "UNEXPECTED_RESPONSE",
      status: 502,
      requestId: "aaaabbbbccccdddd",
      retryable: true,
    });
  });

  it("maps network failures to a retryable NETWORK_ERROR", async () => {
    useHandlers(http.get("*/api/bootstrap", () => HttpResponse.error()));
    const error = await unwrap(api.GET("/api/bootstrap")).catch((reason: unknown) => reason);
    expect(error).toMatchObject({ code: "NETWORK_ERROR", status: 0, retryable: true });
  });

  it("passes aborts through so callers can tell cancellation from failure", async () => {
    useHandlers(mockApi("get", "/api/bootstrap", () => bootstrap()));
    const controller = new AbortController();
    controller.abort();
    const error = await unwrap(api.GET("/api/bootstrap", { signal: controller.signal })).catch(
      (reason: unknown) => reason,
    );
    expect(error).not.toBeInstanceOf(ApiError);
    expect((error as Error).name).toBe("AbortError");
  });

  it("resolves 204 responses without a body", async () => {
    useHandlers(mockApi("delete", "/admin/api/session", () => noContent()));
    await expect(unwrap(api.DELETE("/admin/api/session"))).resolves.toBeUndefined();
  });
});

describe("admin session headers", () => {
  it("sends the CSRF token on unsafe admin requests only", async () => {
    const seen: Record<string, string | null> = {};
    configureApi({ csrfToken: () => "t".repeat(64) });
    useHandlers(
      mockApi("get", "/admin/api/session", ({ request }) => {
        seen.get = request.headers.get("X-CSRF-Token");
        return session();
      }),
      mockApi("delete", "/admin/api/session", ({ request }) => {
        seen.delete = request.headers.get("X-CSRF-Token");
        return noContent();
      }),
      mockApi("get", "/api/bootstrap", ({ request }) => {
        seen.public = request.headers.get("X-CSRF-Token");
        return bootstrap();
      }),
    );
    await unwrap(api.GET("/admin/api/session"));
    await unwrap(api.DELETE("/admin/api/session"));
    await unwrap(api.GET("/api/bootstrap"));
    expect(seen).toEqual({ get: null, delete: "t".repeat(64), public: null });
  });

  it("sends If-Match from the resource revision", async () => {
    let header: string | null = null;
    configureApi({ csrfToken: () => "t".repeat(64) });
    useHandlers(
      mockApi("put", "/admin/api/settings/site", ({ request }) => {
        header = request.headers.get("If-Match");
        return siteSettingsState({ revision: 8 });
      }),
    );
    const current = siteSettingsState({ revision: 7 });
    const { revision: _revision, ...body } = current;
    const saved = await unwrap(
      api.PUT("/admin/api/settings/site", { params: { header: ifMatchHeader(current) }, body }),
    );
    expect(header).toBe('"7"');
    expect(saved.revision).toBe(8);
  });

  it("reports AUTH_REQUIRED from admin requests but not failed sign-ins", async () => {
    const onUnauthorized = vi.fn();
    configureApi({ onUnauthorized });
    useHandlers(
      mockApi("get", "/admin/api/status", () => apiError("AUTH_REQUIRED")),
      mockApi("post", "/admin/api/session", () => apiError("LOGIN_FAILED")),
    );
    await unwrap(api.POST("/admin/api/session", { body: { password: "wrong" } })).catch(() => null);
    expect(onUnauthorized).not.toHaveBeenCalled();
    await unwrap(api.GET("/admin/api/status")).catch(() => null);
    expect(onUnauthorized).toHaveBeenCalledTimes(1);
    expect(onUnauthorized.mock.calls[0]?.[0]).toMatchObject({ code: "AUTH_REQUIRED" });
  });
});

describe("revision helpers", () => {
  it.each([
    ['"7"', 7],
    ['"9007199254740991"', 9007199254740991],
    // Larger revisions cannot be represented exactly in JavaScript.
    ['"123456789012345678"', undefined],
    ["7", undefined],
    ['W/"7"', undefined],
    ['"0"', undefined],
    [null, undefined],
  ])("parses ETag %s", (etag, expected) => {
    expect(revisionFromEtag(etag)).toBe(expected);
  });

  it("quotes revisions and rejects invalid ones", () => {
    expect(ifMatch(7)).toBe('"7"');
    expect(() => ifMatch(0)).toThrow(RangeError);
    expect(() => ifMatch(1.5)).toThrow(RangeError);
  });
});
