import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";
import type { paths } from "@/shared/api";
import { errorCatalog, type CatalogErrorCode } from "@/shared/api/spec.gen";

/** MSW server shared by all tests; unhandled requests fail the test. */
export const server = setupServer();

type Method = "get" | "post" | "put" | "patch" | "delete";
type Operation<P extends keyof paths, M extends Method> = NonNullable<paths[P][M]>;
type PathWith<M extends Method> = {
  [P in keyof paths]: paths[P][M] extends { responses: unknown } ? P : never;
}[keyof paths];
type JsonOf<R> = R extends { content: { "application/json": infer Body } } ? Body : never;
type SuccessStatus = 200 | 201 | 202;
/** JSON body of the operation's success response, from the generated types. */
export type SuccessBody<P extends keyof paths, M extends Method> =
  Operation<P, M> extends { responses: infer R }
    ? JsonOf<R[Extract<keyof R, SuccessStatus>]>
    : never;

export interface ResolverInfo {
  request: Request;
  params: Record<string, string | readonly string[] | undefined>;
}

type Resolver<P extends keyof paths, M extends Method> = (
  info: ResolverInfo,
) => SuccessBody<P, M> | Response | Promise<SuccessBody<P, M> | Response>;

const successStatus: Record<Method, number> = {
  get: 200,
  post: 201,
  put: 200,
  patch: 200,
  delete: 204,
};

/**
 * A typed MSW handler for a spec path: `mockApi("get", "/api/apps/{vendor}/{app}", () => publicApp())`.
 * Returning a plain object sends it as JSON with the method's usual success
 * status (POST 201); return a Response (e.g. `apiError(...)`) for anything else.
 */
export function mockApi<M extends Method, P extends PathWith<M>>(
  method: M,
  path: P,
  resolver: Resolver<P, M>,
  options: { status?: number; once?: boolean } = {},
) {
  const pattern = `*${path.replace(/\{([^}]+)\}/g, ":$1")}`;
  return http[method](
    pattern,
    async ({ request, params }) => {
      const result = await resolver({ request, params });
      if (result instanceof Response) return result;
      return HttpResponse.json(result as never, {
        status: options.status ?? successStatus[method],
        headers: { "X-Request-Id": "0123456789abcdef" },
      });
    },
    { once: options.once ?? false },
  );
}

/** Installs handlers for the current test only. */
export function useHandlers(...handlers: Parameters<typeof server.use>) {
  server.use(...handlers);
}

/** A spec `Error` response with the catalog's status and `retryable`. */
export function apiError(
  code: CatalogErrorCode,
  init: { message?: string; requestId?: string; status?: number } = {},
): Response {
  const entry = errorCatalog[code];
  const requestId = init.requestId ?? "fedcba9876543210";
  return HttpResponse.json(
    {
      error: {
        code,
        message: init.message ?? code,
        request_id: requestId,
        retryable: entry.retryable,
      },
    },
    { status: init.status ?? entry.status, headers: { "X-Request-Id": requestId } },
  );
}

/** 204 No Content. */
export function noContent(): Response {
  return new HttpResponse(null, { status: 204, headers: { "X-Request-Id": "0123456789abcdef" } });
}
