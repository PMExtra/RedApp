import { computed, toValue, type MaybeRefOrGetter } from "vue";
import { useQuery } from "@tanstack/vue-query";
import { api, queryKey, unwrap } from "@/shared/api";
import type { HistoryRange, MetricKey } from "./catalog";

/** Whose metrics: the whole service or one application (`vendor/app`). */
export type MetricScope = { kind: "global" } | { kind: "app"; vendor: string; app: string };

export const GLOBAL_SCOPE: MetricScope = { kind: "global" };

export function appScope(vendor: string, app: string): MetricScope {
  return { kind: "app", vendor, app };
}

export interface StatusQueryOptions {
  /** From `useAutoRefresh().refetchInterval`; `false` disables polling. */
  refetchInterval?: MaybeRefOrGetter<number | false>;
  enabled?: MaybeRefOrGetter<boolean>;
}

/**
 * `getStatus`. On a failed refetch TanStack keeps the last good snapshot in
 * `data`, so pages show it with a warning instead of replacing it.
 */
export function useGlobalStatus(options: StatusQueryOptions = {}) {
  return useQuery({
    queryKey: queryKey("getStatus"),
    queryFn: ({ signal }) => unwrap(api.GET("/admin/api/status", { signal })),
    refetchInterval: computed(() => toValue(options.refetchInterval) ?? false),
    enabled: computed(() => toValue(options.enabled) ?? true),
    staleTime: 0,
  });
}

/** `getAppStatus` (http-cache, codex and claude-code applications). */
export function useAppStatus(
  vendor: MaybeRefOrGetter<string>,
  app: MaybeRefOrGetter<string>,
  options: StatusQueryOptions = {},
) {
  return useQuery({
    queryKey: computed(() =>
      queryKey("getAppStatus", { vendor: toValue(vendor), app: toValue(app) }),
    ),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/apps/{vendor}/{app}/status", {
          params: { path: { vendor: toValue(vendor), app: toValue(app) } },
          signal,
        }),
      ),
    refetchInterval: computed(() => toValue(options.refetchInterval) ?? false),
    enabled: computed(() => toValue(options.enabled) ?? true),
    staleTime: 0,
  });
}

function fetchHistory(
  scope: MetricScope,
  metric: MetricKey,
  range: HistoryRange,
  signal: AbortSignal,
) {
  if (scope.kind === "global") {
    return unwrap(api.GET("/admin/api/history", { params: { query: { metric, range } }, signal }));
  }
  return unwrap(
    api.GET("/admin/api/apps/{vendor}/{app}/history", {
      params: { path: { vendor: scope.vendor, app: scope.app }, query: { metric, range } },
      signal,
    }),
  );
}

/** `getHistory` or `getAppHistory`, depending on the scope. */
export function useMetricHistory(
  scope: MaybeRefOrGetter<MetricScope>,
  metric: MaybeRefOrGetter<MetricKey>,
  range: MaybeRefOrGetter<HistoryRange>,
) {
  return useQuery({
    queryKey: computed(() => {
      const current = toValue(scope);
      const params = { metric: toValue(metric), range: toValue(range) };
      return current.kind === "global"
        ? queryKey("getHistory", params)
        : queryKey("getAppHistory", { vendor: current.vendor, app: current.app, ...params });
    }),
    queryFn: ({ signal }) => fetchHistory(toValue(scope), toValue(metric), toValue(range), signal),
    // Samples are taken once per minute.
    staleTime: 60_000,
  });
}
