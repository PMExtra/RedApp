import { computed, toValue, type MaybeRefOrGetter, type Ref } from "vue";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/vue-query";
import { api, ifMatch, queryKey, unwrap, type Schema } from "@/shared/api";

export type RetentionPreview = Schema<"RetentionPreview">;

type Name = MaybeRefOrGetter<string>;

export function useRetentionStatus(vendor: Name, app: Name) {
  return useQuery({
    queryKey: computed(() =>
      queryKey("getRetentionStatus", { vendor: toValue(vendor), app: toValue(app) }),
    ),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/apps/{vendor}/{app}/retention/status", {
          params: { path: { vendor: toValue(vendor), app: toValue(app) } },
          signal,
        }),
      ),
  });
}

function previewKey(vendor: string, app: string, id: string) {
  return queryKey("getRetentionPreview", { vendor, app, preview_id: id });
}

/** The current preview (and, after execution, its receipt) by ID. */
export function useRetentionPreview(vendor: Name, app: Name, id: Ref<string | null>) {
  return useQuery({
    queryKey: computed(() => previewKey(toValue(vendor), toValue(app), id.value ?? "")),
    enabled: computed(() => id.value !== null),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/apps/{vendor}/{app}/retention/{preview_id}", {
          params: {
            path: { vendor: toValue(vendor), app: toValue(app), preview_id: id.value ?? "" },
          },
          signal,
        }),
      ),
  });
}

export function useRetentionItems(
  vendor: Name,
  app: Name,
  id: Ref<string | null>,
  page: Ref<number>,
) {
  return useQuery({
    queryKey: computed(() =>
      queryKey("listRetentionPreviewItems", {
        vendor: toValue(vendor),
        app: toValue(app),
        preview_id: id.value,
        page: page.value,
      }),
    ),
    enabled: computed(() => id.value !== null),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/apps/{vendor}/{app}/retention/{preview_id}/items", {
          params: {
            path: { vendor: toValue(vendor), app: toValue(app), preview_id: id.value ?? "" },
            query: { page: page.value, limit: 25 },
          },
          signal,
        }),
      ),
    placeholderData: keepPreviousData,
  });
}

/**
 * Preview with the saved policy (`If-Match` = configuration revision), then
 * execute by preview ID. Both responses seed the preview query.
 */
export function useRetentionActions(vendor: Name, app: Name) {
  const queryClient = useQueryClient();
  const path = () => ({ vendor: toValue(vendor), app: toValue(app) });
  const remember = (preview: RetentionPreview) => {
    queryClient.setQueryData(previewKey(path().vendor, path().app, preview.id), preview);
  };
  const preview = useMutation({
    meta: { handledCodes: ["REVISION_CONFLICT"] },
    mutationFn: (revision: number) =>
      unwrap(
        api.POST("/admin/api/apps/{vendor}/{app}/retention/preview", {
          params: { path: path(), header: { "If-Match": ifMatch(revision) } },
        }),
      ),
    onSuccess: remember,
  });
  const execute = useMutation({
    meta: { handledCodes: ["PREVIEW_NOT_FOUND", "PREVIEW_STALE"] },
    mutationFn: (id: string) =>
      unwrap(
        api.POST("/admin/api/apps/{vendor}/{app}/retention/{preview_id}/execute", {
          params: { path: { ...path(), preview_id: id } },
        }),
      ),
    onSuccess: (result) => {
      remember(result);
      for (const operation of [
        "getRetentionStatus",
        "listRetentionPreviewItems",
        "listVersions",
        "listResources",
        "listSources",
        "getAppStatus",
      ] as const) {
        void queryClient.invalidateQueries({ queryKey: [operation, path()] });
      }
    },
  });
  return { preview, execute };
}
