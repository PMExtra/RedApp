import { computed, ref, toValue, type MaybeRefOrGetter } from "vue";
import { useMutation, useQueryClient, type QueryKey } from "@tanstack/vue-query";
import { isApiError, type AnyErrorCode, type ApiError } from "./errors";
import { ifMatch } from "./revision";

export interface RevisionedMutationOptions<TData, TVariables> {
  /** Revision of the baseline the draft was made from (the last successful read). */
  revision: MaybeRefOrGetter<number | undefined>;
  /** Performs the write; pass `ifMatch` as the `If-Match` header. */
  mutationFn: (variables: TVariables, ifMatch: string) => Promise<TData>;
  /**
   * Query holding the resource. The response (always the complete new state)
   * replaces its cached value, and `reload()` refetches it.
   */
  queryKey?: MaybeRefOrGetter<QueryKey | undefined>;
  onSuccess?: (data: TData, variables: TVariables) => void;
  /** Further error codes the caller presents itself (no global error toast). */
  handledCodes?: readonly AnyErrorCode[];
}

/**
 * A conditional write of a revisioned resource. A 409 REVISION_CONFLICT does
 * not toast: it sets `conflict` so the page can keep the user's draft and show
 * `RevisionConflictAlert` with a reload action. Other errors use the global
 * mutation error handler.
 */
export function useRevisionedMutation<TData, TVariables = void>(
  options: RevisionedMutationOptions<TData, TVariables>,
) {
  const queryClient = useQueryClient();
  const conflict = ref<ApiError | null>(null);

  const mutation = useMutation<TData, Error, TVariables>({
    meta: { handledCodes: ["REVISION_CONFLICT", ...(options.handledCodes ?? [])] },
    mutationFn: (variables) => {
      const revision = toValue(options.revision);
      if (revision === undefined) {
        return Promise.reject(new Error("useRevisionedMutation: no baseline revision"));
      }
      conflict.value = null;
      return options.mutationFn(variables, ifMatch(revision));
    },
    onSuccess: (data, variables) => {
      const key = toValue(options.queryKey);
      if (key) queryClient.setQueryData(key, data);
      options.onSuccess?.(data, variables);
    },
    onError: (error) => {
      if (isApiError(error, "REVISION_CONFLICT")) conflict.value = error;
    },
  });

  /** Refetches the baseline. The caller decides whether to reset its draft. */
  async function reload(): Promise<void> {
    conflict.value = null;
    const key = toValue(options.queryKey);
    if (key) await queryClient.refetchQueries({ queryKey: key, exact: true });
  }

  return {
    ...mutation,
    conflict: computed(() => conflict.value),
    hasConflict: computed(() => conflict.value !== null),
    reload,
    dismissConflict: () => {
      conflict.value = null;
    },
  };
}
