import { computed, toValue, type MaybeRefOrGetter } from "vue";
import { keepPreviousData, useQuery } from "@tanstack/vue-query";
import { api, queryKey, unwrap, type Schema } from "@/shared/api";

export type OperationalEvent = Schema<"Event">;

export const EVENTS_PAGE_SIZE = 50;

/** An application whose events are listed instead of all events. */
export interface EventsApp {
  vendor: string;
  app: string;
}

/**
 * One cursor page of `listEvents`, or of `listAppEvents` with `app` (newest
 * first). Failed refetches keep the last page in `data`.
 */
export function useEvents(
  cursor: MaybeRefOrGetter<string | null>,
  options: {
    refetchInterval?: MaybeRefOrGetter<number | false>;
    app?: MaybeRefOrGetter<EventsApp | undefined>;
  } = {},
) {
  return useQuery({
    queryKey: computed(() => {
      const app = toValue(options.app);
      const query = { limit: EVENTS_PAGE_SIZE, cursor: toValue(cursor) };
      return app ? queryKey("listAppEvents", { ...app, ...query }) : queryKey("listEvents", query);
    }),
    queryFn: ({ signal }) => {
      const current = toValue(cursor);
      const app = toValue(options.app);
      const query = { limit: EVENTS_PAGE_SIZE, ...(current ? { cursor: current } : {}) };
      return unwrap(
        app
          ? api.GET("/admin/api/apps/{vendor}/{app}/events", {
              params: { path: app, query },
              signal,
            })
          : api.GET("/admin/api/events", { params: { query }, signal }),
      );
    },
    placeholderData: keepPreviousData,
    refetchInterval: computed(() => toValue(options.refetchInterval) ?? false),
    staleTime: 0,
  });
}
