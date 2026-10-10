import { computed, toValue, type MaybeRefOrGetter, type Ref } from "vue";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/vue-query";
import { api, queryKey, unwrap, type Schema } from "@/shared/api";

export type AdminApp = Schema<"App">;
export type ProviderKey = Schema<"ProviderKey">;
export type Metric = Schema<"Metric">;
export type MetricKey = Schema<"MetricKey">;
export type HistoryRange = "24h" | "7d" | "30d";
export type SourceEpoch = Schema<"SourceEpoch">;
export type VersionCleanupPreview = Schema<"VersionCleanupPreview">;

type Name = MaybeRefOrGetter<string>;

/** Providers with versions, resources, retention and release prewarm. */
export function hasVersions(provider: ProviderKey | undefined): boolean {
  return provider === "codex" || provider === "claude-code";
}

/** Providers with a cache tab (`x-spa-routes`): HTTP cache and release apps. */
export function hasCache(provider: ProviderKey | undefined): boolean {
  return provider === "http-cache" || hasVersions(provider);
}

/**
 * The administrator view of an application (`getApp`). Package B's layout
 * reads the same key, so the tabs share one cached copy.
 */
export function useAdminApp(vendor: Name, app: Name) {
  return useQuery({
    queryKey: computed(() => queryKey("getApp", { vendor: toValue(vendor), app: toValue(app) })),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/apps/{vendor}/{app}", {
          params: { path: { vendor: toValue(vendor), app: toValue(app) } },
          signal,
        }),
      ),
  });
}

export function useAppStatus(vendor: Name, app: Name, refetchInterval: Ref<number | false>) {
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
    refetchInterval,
    staleTime: 0,
  });
}

export function useAppHistory(
  vendor: Name,
  app: Name,
  metric: MaybeRefOrGetter<MetricKey | undefined>,
  range: MaybeRefOrGetter<HistoryRange>,
) {
  return useQuery({
    queryKey: computed(() =>
      queryKey("getAppHistory", {
        vendor: toValue(vendor),
        app: toValue(app),
        metric: toValue(metric),
        range: toValue(range),
      }),
    ),
    enabled: computed(() => toValue(metric) !== undefined),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/apps/{vendor}/{app}/history", {
          params: {
            path: { vendor: toValue(vendor), app: toValue(app) },
            query: { metric: toValue(metric) as MetricKey, range: toValue(range) },
          },
          signal,
        }),
      ),
  });
}

export function useVersions(
  vendor: Name,
  app: Name,
  cursor: Ref<string | null>,
  refetchInterval: Ref<number | false>,
) {
  return useQuery({
    queryKey: computed(() =>
      queryKey("listVersions", {
        vendor: toValue(vendor),
        app: toValue(app),
        cursor: cursor.value,
      }),
    ),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/apps/{vendor}/{app}/versions", {
          params: {
            path: { vendor: toValue(vendor), app: toValue(app) },
            query: { limit: 25, ...(cursor.value ? { cursor: cursor.value } : {}) },
          },
          signal,
        }),
      ),
    placeholderData: keepPreviousData,
    refetchInterval,
  });
}

export function useResources(
  vendor: Name,
  app: Name,
  version: Ref<string | null>,
  cursor: Ref<string | null>,
  refetchInterval: Ref<number | false>,
) {
  return useQuery({
    queryKey: computed(() =>
      queryKey("listResources", {
        vendor: toValue(vendor),
        app: toValue(app),
        version: version.value,
        cursor: cursor.value,
      }),
    ),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/apps/{vendor}/{app}/resources", {
          params: {
            path: { vendor: toValue(vendor), app: toValue(app) },
            query: {
              limit: 25,
              ...(version.value ? { version: version.value } : {}),
              ...(cursor.value ? { cursor: cursor.value } : {}),
            },
          },
          signal,
        }),
      ),
    placeholderData: keepPreviousData,
    refetchInterval,
  });
}

/** Source epochs of an application, oldest first (`listSources`). */
export function useSources(vendor: Name, app: Name) {
  return useQuery({
    queryKey: computed(() =>
      queryKey("listSources", { vendor: toValue(vendor), app: toValue(app) }),
    ),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/apps/{vendor}/{app}/sources", {
          params: { path: { vendor: toValue(vendor), app: toValue(app) } },
          signal,
        }),
      ),
  });
}

/** Version cleanup: a frozen preview, then execution by preview ID. */
export function useVersionCleanup(vendor: Name, app: Name) {
  const queryClient = useQueryClient();
  const path = () => ({ vendor: toValue(vendor), app: toValue(app) });
  const preview = useMutation({
    mutationFn: (body: Schema<"VersionCleanupRequest">) =>
      unwrap(
        api.POST("/admin/api/apps/{vendor}/{app}/version-cleanup/preview", {
          params: { path: path() },
          body,
        }),
      ),
  });
  const execute = useMutation({
    meta: { handledCodes: ["PREVIEW_NOT_FOUND", "PREVIEW_STALE"] },
    mutationFn: (previewId: string) =>
      unwrap(
        api.POST("/admin/api/apps/{vendor}/{app}/version-cleanup/{preview_id}/execute", {
          params: { path: { ...path(), preview_id: previewId } },
        }),
      ),
    onSuccess: () => {
      for (const operation of ["listResources", "listVersions", "getAppStatus"] as const) {
        void queryClient.invalidateQueries({ queryKey: [operation, path()] });
      }
    },
  });
  return { preview, execute };
}
