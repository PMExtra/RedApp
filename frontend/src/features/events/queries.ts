import { computed, toValue, type MaybeRefOrGetter } from "vue";
import { keepPreviousData, useQuery } from "@tanstack/vue-query";
import { api, queryKey, unwrap, type Schema } from "@/shared/api";

export type OperationalEvent = Schema<"Event">;

export const EVENTS_PAGE_SIZE = 50;

/**
 * One cursor page of `listEvents` (newest first). Failed refetches keep the
 * last page in `data`.
 */
export function useEvents(
  cursor: MaybeRefOrGetter<string | null>,
  options: { refetchInterval?: MaybeRefOrGetter<number | false> } = {},
) {
  return useQuery({
    queryKey: computed(() =>
      queryKey("listEvents", { limit: EVENTS_PAGE_SIZE, cursor: toValue(cursor) }),
    ),
    queryFn: ({ signal }) => {
      const current = toValue(cursor);
      return unwrap(
        api.GET("/admin/api/events", {
          params: {
            query: { limit: EVENTS_PAGE_SIZE, ...(current ? { cursor: current } : {}) },
          },
          signal,
        }),
      );
    },
    placeholderData: keepPreviousData,
    refetchInterval: computed(() => toValue(options.refetchInterval) ?? false),
    staleTime: 0,
  });
}
