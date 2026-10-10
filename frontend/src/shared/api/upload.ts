import { csrfHeaders, reportUnauthorized } from "./client";
import { ApiError, apiErrorFromBody, networkError } from "./errors";

export interface UploadProgress {
  loaded: number;
  /** `null` when the browser cannot compute the total. */
  total: number | null;
}

export interface UploadOptions {
  url: string;
  method?: "POST" | "PUT";
  body: FormData | Blob;
  headers?: Record<string, string>;
  signal?: AbortSignal;
  onProgress?: (progress: UploadProgress) => void;
}

/**
 * Multipart or binary upload with progress (fetch cannot report upload
 * progress). Adds the CSRF header like the typed client and rejects with
 * ApiError; an abort rejects with an AbortError DOMException. The caller
 * types the JSON response with the generated schema types.
 */
export function uploadWithProgress<T>(options: UploadOptions): Promise<T> {
  const method = options.method ?? "POST";
  return new Promise<T>((resolve, reject) => {
    if (options.signal?.aborted) {
      reject(new DOMException("Upload aborted", "AbortError"));
      return;
    }
    const request = new XMLHttpRequest();
    request.open(method, options.url);
    request.withCredentials = true;
    request.responseType = "text";
    const headers = { ...options.headers, ...csrfHeaders(method, options.url) };
    for (const [name, value] of Object.entries(headers)) request.setRequestHeader(name, value);
    const abort = () => {
      request.abort();
    };
    options.signal?.addEventListener("abort", abort, { once: true });
    request.upload.addEventListener("progress", (event) => {
      options.onProgress?.({
        loaded: event.loaded,
        total: event.lengthComputable ? event.total : null,
      });
    });
    request.addEventListener("abort", () => {
      reject(new DOMException("Upload aborted", "AbortError"));
    });
    request.addEventListener("error", () => {
      reject(networkError(new Error("Upload failed")));
    });
    request.addEventListener("load", () => {
      options.signal?.removeEventListener("abort", abort);
      let body: unknown;
      try {
        body = request.responseText ? JSON.parse(request.responseText) : undefined;
      } catch {
        body = undefined;
      }
      if (request.status >= 200 && request.status < 300) {
        resolve(body as T);
        return;
      }
      const responseHeaders = new Headers();
      const requestId = request.getResponseHeader("X-Request-Id");
      if (requestId) responseHeaders.set("X-Request-Id", requestId);
      const error: ApiError = apiErrorFromBody(request.status, body, responseHeaders);
      reportUnauthorized(options.url, error);
      reject(error);
    });
    request.send(options.body);
  });
}
