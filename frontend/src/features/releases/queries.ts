import { computed, toValue, type MaybeRefOrGetter, type Ref } from "vue";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/vue-query";
import { api, queryKey, unwrap, type Schema } from "@/shared/api";

export type SourceEpoch = Schema<"SourceEpoch">;
export type VersionCleanupPreview = Schema<"VersionCleanupPreview">;

type Name = MaybeRefOrGetter<string>;

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
