import type { components } from "./schema.gen";
import { errorCatalog } from "./spec.gen";

/** Error codes of the spec's catalog (`components.x-error-codes`). */
export type ErrorCode = components["schemas"]["ErrorCode"];
/** Codes produced by the client when no spec error body is available. */
export type ClientErrorCode = "NETWORK_ERROR" | "UNEXPECTED_RESPONSE";
export type AnyErrorCode = ErrorCode | ClientErrorCode;

/** Every failed API call rejects with an ApiError; branch on `code` only. */
export class ApiError extends Error {
  override readonly name = "ApiError";
  readonly code: AnyErrorCode;
  /** HTTP status; 0 when the server was not reached. */
  readonly status: number;
  /** `X-Request-Id` of the failed response, for support and logs. */
  readonly requestId: string | null;
  /** The same request may succeed later without changes. */
  readonly retryable: boolean;

  constructor(init: {
    code: AnyErrorCode;
    message: string;
    status: number;
    requestId?: string | null;
    retryable?: boolean;
  }) {
    super(init.message);
    this.code = init.code;
    this.status = init.status;
    this.requestId = init.requestId ?? null;
    this.retryable = init.retryable ?? false;
  }

  is(...codes: AnyErrorCode[]): boolean {
    return codes.includes(this.code);
  }
}

export function isApiError(value: unknown, ...codes: AnyErrorCode[]): value is ApiError {
  return value instanceof ApiError && (codes.length === 0 || codes.includes(value.code));
}

export function isAbortError(value: unknown): boolean {
  return value instanceof DOMException
    ? value.name === "AbortError"
    : value instanceof Error && value.name === "AbortError";
}

function isErrorCode(value: unknown): value is ErrorCode {
  return typeof value === "string" && Object.hasOwn(errorCatalog, value);
}

/**
 * Builds an ApiError from a non-2xx response. A body that does not match the
 * spec's `Error` schema (for example an HTML page from a proxy) becomes
 * UNEXPECTED_RESPONSE; the request ID then comes from the header.
 */
export function apiErrorFromBody(status: number, body: unknown, headers?: Headers): ApiError {
  const headerId = headers?.get("X-Request-Id") ?? null;
  const error =
    typeof body === "object" && body !== null && "error" in body ? body.error : undefined;
  if (typeof error === "object" && error !== null) {
    const { code, message, request_id, retryable } = error as Record<string, unknown>;
    if (isErrorCode(code)) {
      return new ApiError({
        code,
        message: typeof message === "string" ? message : code,
        status,
        requestId: typeof request_id === "string" ? request_id : headerId,
        retryable: typeof retryable === "boolean" ? retryable : errorCatalog[code].retryable,
      });
    }
  }
  return new ApiError({
    code: "UNEXPECTED_RESPONSE",
    message: `Unexpected response (HTTP ${status})`,
    status,
    requestId: headerId,
    retryable: status >= 500,
  });
}

export function networkError(cause: unknown): ApiError {
  const error = new ApiError({
    code: "NETWORK_ERROR",
    message: cause instanceof Error ? cause.message : "Network error",
    status: 0,
    retryable: true,
  });
  return error;
}
