import { useMutation } from "@tanstack/vue-query";
import { api, unwrap, type Schema } from "@/shared/api";

export type PathMatch = Schema<"PathMatch">;
export type CacheRule = Schema<"CacheRule">;
export type CleanupRule = Schema<"CleanupRule">;
export type CleanupBasis = Schema<"CleanupBasis">;

/** Patterns may contain at most 1024 UTF-8 bytes. */
export const MAX_PATTERN_BYTES = 1024;

export function patternTooLong(pattern: string): boolean {
  return new TextEncoder().encode(pattern).length > MAX_PATTERN_BYTES;
}

/** Server-side evaluation of a glob or RE2 pattern (`testPathMatch`). */
export function useTestPathMatch() {
  return useMutation({
    meta: { handledCodes: ["VALIDATION_FAILED"] },
    mutationFn: (body: Schema<"PathMatchTest">) =>
      unwrap(api.POST("/admin/api/path-match", { body })),
  });
}
