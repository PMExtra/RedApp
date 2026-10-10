import createClient, { type Middleware } from "openapi-fetch";
import { ApiError, apiErrorFromBody, isAbortError, networkError } from "./errors";
import type { paths } from "./schema.gen";

export type { paths, components, operations } from "./schema.gen";

export interface ApiHooks {
  /** CSRF token of the current admin session (kept in memory only). */
  csrfToken?: () => string | null | undefined;
  /** Called when an admin request returns 401 AUTH_REQUIRED. */
  onUnauthorized?: (error: ApiError) => void;
}

const hooks: ApiHooks = {};

/** Installed by the admin entry; the public entry needs no hooks. */
export function configureApi(next: ApiHooks): void {
  hooks.csrfToken = next.csrfToken;
  hooks.onUnauthorized = next.onUnauthorized;
}

const UNSAFE_METHODS = new Set(["POST", "PUT", "PATCH", "DELETE"]);

export function isAdminApi(url: string): boolean {
  return new URL(url, "http://redapp.invalid").pathname.startsWith("/admin/api/");
}

/** Adds `X-CSRF-Token` to unsafe admin requests. */
export function csrfHeaders(method: string, url: string): Record<string, string> {
  if (!UNSAFE_METHODS.has(method.toUpperCase()) || !isAdminApi(url)) return {};
  const token = hooks.csrfToken?.();
  return token ? { "X-CSRF-Token": token } : {};
}

/** Reports AUTH_REQUIRED from admin requests (sign-in failures use LOGIN_FAILED). */
export function reportUnauthorized(url: string, error: ApiError): void {
  if (error.status === 401 && error.code === "AUTH_REQUIRED" && isAdminApi(url)) {
    hooks.onUnauthorized?.(error);
  }
}

const sessionMiddleware: Middleware = {
  onRequest({ request }) {
    for (const [name, value] of Object.entries(csrfHeaders(request.method, request.url))) {
      request.headers.set(name, value);
    }
    return request;
  },
  async onResponse({ request, response }) {
    if (response.status === 401) {
      const body: unknown = await response
        .clone()
        .json()
        .catch(() => undefined);
      reportUnauthorized(request.url, apiErrorFromBody(401, body, response.headers));
    }
    return response;
  },
};

/**
 * The typed API client. Paths, parameters and bodies come from the spec:
 * `api.GET("/api/apps/{vendor}/{app}", { params: { path: { vendor, app } }, signal })`.
 * Wrap calls in `unwrap()` to get the data or an ApiError.
 */
export const api = createClient<paths>({
  baseUrl: globalThis.location.origin,
  credentials: "same-origin",
  // Resolve fetch per call so test interceptors installed later apply.
  fetch: (input) => globalThis.fetch(input),
});
api.use(sessionMiddleware);

interface ClientResult {
  data?: unknown;
  error?: unknown;
  response: Response;
}
type Data<R extends ClientResult> = Exclude<R["data"], undefined>;
/** `undefined` for responses without a body (204). */
export type UnwrappedData<R extends ClientResult> = [Data<R>] extends [never] ? undefined : Data<R>;

/**
 * Resolves with the response data or rejects with ApiError. Aborted requests
 * reject with the original AbortError so TanStack Query treats them as cancelled.
 */
export async function unwrap<R extends ClientResult>(
  pending: Promise<R>,
): Promise<UnwrappedData<R>> {
  let result: R;
  try {
    result = await pending;
  } catch (cause) {
    if (isAbortError(cause) || cause instanceof ApiError) throw cause;
    throw networkError(cause);
  }
  if (result.response.ok) return result.data as UnwrappedData<R>;
  throw apiErrorFromBody(result.response.status, result.error, result.response.headers);
}
