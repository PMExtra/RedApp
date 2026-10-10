import { MutationCache, QueryClient, type QueryKey } from "@tanstack/vue-query";
import { isApiError, type AnyErrorCode } from "./errors";
import type { operations } from "./schema.gen";

/** operationId of the spec; the first element of every query key. */
export type OperationId = keyof operations;

declare module "@tanstack/vue-query" {
  interface Register {
    mutationMeta: {
      /** Error codes the caller presents itself; no global error toast for them. */
      handledCodes?: readonly AnyErrorCode[];
      /** Never show the global error toast. */
      silent?: boolean;
    };
  }
}

/**
 * Query keys are `[operationId]` or `[operationId, params]`, where params are the
 * path and query parameters of the operation. Invalidate a whole operation with
 * `{ queryKey: [operationId] }` or one application with
 * `{ queryKey: [operationId, { vendor, app }] }` (TanStack matches partially).
 */
export function queryKey(operation: OperationId, params?: Record<string, unknown>): QueryKey {
  return params === undefined ? [operation] : [operation, params];
}

export interface QueryClientOptions {
  /** Global handler for failed mutations (the app shows an error toast). */
  onMutationError?: (error: unknown) => void;
}

/** Retries only errors the catalog marks retryable, at most twice. */
export function shouldRetry(failureCount: number, error: unknown): boolean {
  return isApiError(error) && error.retryable && failureCount < 2;
}

export function createQueryClient(options: QueryClientOptions = {}): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 15_000,
        retry: shouldRetry,
        refetchOnWindowFocus: true,
      },
      mutations: { retry: false },
    },
    mutationCache: new MutationCache({
      onError: (error, _variables, _context, mutation) => {
        const meta = mutation.meta;
        if (meta?.silent) return;
        if (isApiError(error) && meta?.handledCodes?.includes(error.code)) return;
        options.onMutationError?.(error);
      },
    }),
  });
}
