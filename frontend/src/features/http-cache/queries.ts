import { computed, toValue, type MaybeRefOrGetter, type Ref } from "vue";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/vue-query";
import { api, queryKey, unwrap, type Schema } from "@/shared/api";

export type CacheEntry = Schema<"CacheEntry">;
export type MaintenanceKind = "refresh" | "cleanup";
export type MaintenancePreview = Schema<"MaintenancePreview">;
export type CleanupRequest = Schema<"CacheCleanupPreviewRequest">;
/** Refresh previews use only `match`; cleanup previews need the other fields. */
export type MaintenanceRequest = Pick<CleanupRequest, "match"> & Partial<CleanupRequest>;

/** Refresh and cleanup previews are polled every 5 seconds while building or running. */
export const MAINTENANCE_POLL_MS = 5_000;

type Name = MaybeRefOrGetter<string>;

export function isActive(preview: MaintenancePreview | undefined): boolean {
  return preview?.state === "building" || preview?.state === "running";
}

export function useCacheEntries(
  vendor: Name,
  app: Name,
  sourceEpoch: Ref<number | null>,
  cursor: Ref<string | null>,
) {
  return useQuery({
    queryKey: computed(() =>
      queryKey("listCacheEntries", {
        vendor: toValue(vendor),
        app: toValue(app),
        source_epoch: sourceEpoch.value,
        cursor: cursor.value,
      }),
    ),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/apps/{vendor}/{app}/cache/entries", {
          params: {
            path: { vendor: toValue(vendor), app: toValue(app) },
            query: {
              limit: 50,
              ...(sourceEpoch.value === null ? {} : { source_epoch: sourceEpoch.value }),
              ...(cursor.value ? { cursor: cursor.value } : {}),
            },
          },
          signal,
        }),
      ),
    placeholderData: keepPreviousData,
  });
}

/** Revalidates one cached file now; a failed revalidation is a result, not an error. */
export function useRefreshEntry(vendor: Name, app: Name) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (path: string) =>
      unwrap(
        api.POST("/admin/api/apps/{vendor}/{app}/cache/refresh", {
          params: { path: { vendor: toValue(vendor), app: toValue(app) } },
          body: { path },
        }),
      ),
    onSettled: () =>
      queryClient.invalidateQueries({
        queryKey: ["listCacheEntries", { vendor: toValue(vendor), app: toValue(app) }],
      }),
  });
}

export function useAutoCleanupStatus() {
  return useQuery({
    queryKey: queryKey("getAutoCleanupStatus"),
    queryFn: ({ signal }) => unwrap(api.GET("/admin/api/cache/auto-cleanup", { signal })),
  });
}

function jobKey(kind: MaintenanceKind, vendor: string, app: string, id: string | null) {
  return queryKey(kind === "refresh" ? "getCacheRefresh" : "getCacheCleanup", {
    vendor,
    app,
    preview_id: id,
  });
}

/** A refresh or cleanup preview by ID; polled while it builds or runs. */
export function useMaintenanceJob(
  kind: MaintenanceKind,
  vendor: Name,
  app: Name,
  id: Ref<string | null>,
) {
  return useQuery({
    queryKey: computed(() => jobKey(kind, toValue(vendor), toValue(app), id.value)),
    enabled: computed(() => id.value !== null),
    queryFn: ({ signal }) => {
      const path = { vendor: toValue(vendor), app: toValue(app), preview_id: id.value ?? "" };
      return kind === "refresh"
        ? unwrap(
            api.GET("/admin/api/apps/{vendor}/{app}/cache/refresh/{preview_id}", {
              params: { path },
              signal,
            }),
          )
        : unwrap(
            api.GET("/admin/api/apps/{vendor}/{app}/cache/cleanup/{preview_id}", {
              params: { path },
              signal,
            }),
          );
    },
    staleTime: 0,
    refetchInterval: (query) => (isActive(query.state.data) ? MAINTENANCE_POLL_MS : false),
  });
}

export function useMaintenanceItems(
  kind: MaintenanceKind,
  vendor: Name,
  app: Name,
  id: Ref<string | null>,
  cursor: Ref<string | null>,
  enabled: Ref<boolean>,
  refetchInterval: Ref<number | false>,
) {
  return useQuery({
    queryKey: computed(() =>
      queryKey(kind === "refresh" ? "listCacheRefreshItems" : "listCacheCleanupItems", {
        vendor: toValue(vendor),
        app: toValue(app),
        preview_id: id.value,
        cursor: cursor.value,
      }),
    ),
    enabled: computed(() => id.value !== null && enabled.value),
    queryFn: ({ signal }) => {
      const params = {
        path: { vendor: toValue(vendor), app: toValue(app), preview_id: id.value ?? "" },
        query: { limit: 25, ...(cursor.value ? { cursor: cursor.value } : {}) },
      };
      return kind === "refresh"
        ? unwrap(
            api.GET("/admin/api/apps/{vendor}/{app}/cache/refresh/{preview_id}/items", {
              params,
              signal,
            }),
          )
        : unwrap(
            api.GET("/admin/api/apps/{vendor}/{app}/cache/cleanup/{preview_id}/items", {
              params,
              signal,
            }),
          );
    },
    staleTime: 0,
    placeholderData: keepPreviousData,
    refetchInterval,
  });
}

/** Create and execute refresh or cleanup previews; responses seed the job query. */
export function useMaintenanceActions(kind: MaintenanceKind, vendor: Name, app: Name) {
  const queryClient = useQueryClient();
  const path = () => ({ vendor: toValue(vendor), app: toValue(app) });
  const remember = (preview: MaintenancePreview) => {
    queryClient.setQueryData(jobKey(kind, path().vendor, path().app, preview.id), preview);
  };
  const create = useMutation({
    mutationFn: (body: MaintenanceRequest) =>
      kind === "refresh"
        ? unwrap(
            api.POST("/admin/api/apps/{vendor}/{app}/cache/refresh/preview", {
              params: { path: path() },
              body: { match: body.match ?? { type: "glob", pattern: "/" } },
            }),
          )
        : unwrap(
            api.POST("/admin/api/apps/{vendor}/{app}/cache/cleanup/preview", {
              params: { path: path() },
              body: body as CleanupRequest,
            }),
          ),
    onSuccess: remember,
  });
  const execute = useMutation({
    meta: { handledCodes: ["PREVIEW_NOT_FOUND", "PREVIEW_STALE", "OPERATION_IN_PROGRESS"] },
    mutationFn: (id: string) => {
      const params = { path: { ...path(), preview_id: id } };
      return kind === "refresh"
        ? unwrap(
            api.POST("/admin/api/apps/{vendor}/{app}/cache/refresh/{preview_id}/execute", {
              params,
            }),
          )
        : unwrap(
            api.POST("/admin/api/apps/{vendor}/{app}/cache/cleanup/{preview_id}/execute", {
              params,
            }),
          );
    },
    onSuccess: (preview) => {
      remember(preview);
      void queryClient.invalidateQueries({ queryKey: ["listCacheEntries", path()] });
    },
  });
  return { create, execute };
}
