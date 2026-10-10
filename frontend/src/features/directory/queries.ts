import { computed, toValue, type MaybeRefOrGetter } from "vue";
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
  type QueryKey,
} from "@tanstack/vue-query";
import {
  api,
  ifMatch,
  isApiError,
  queryKey,
  unwrap,
  uploadWithProgress,
  type Schema,
  type UploadProgress,
} from "@/shared/api";
import { appConfigurationKey, vendorConfigurationKey } from "@/features/configuration";
import { toast } from "@/shared/lib";
import { translate } from "@/shared/i18n";

export type Vendor = Schema<"Vendor">;
export type VendorListItem = Schema<"VendorListItem">;
export type App = Schema<"App">;
export type AppListItem = Schema<"AppListItem">;
export type Provider = Schema<"Provider">;
export type ProviderKey = Schema<"ProviderKey">;
export type DirectoryState = Schema<"DirectoryState">;
export type AppSort = "name" | "version" | "updated" | "downloads";

/** Page sizes of the directory lists (the spec defaults). */
export const VENDOR_PAGE_SIZE = 12;
export const APP_PAGE_SIZE = 20;
const ALL_PAGE_SIZE = 100;

export function vendorKey(vendor: string) {
  return queryKey("getVendor", { vendor });
}

export function appKey(vendor: string, app: string) {
  return queryKey("getApp", { vendor, app });
}

/** Providers never change while the server runs. */
export function useProviders() {
  return useQuery({
    queryKey: queryKey("listProviders"),
    queryFn: ({ signal }) => unwrap(api.GET("/admin/api/providers", { signal })),
    staleTime: Number.POSITIVE_INFINITY,
  });
}

export interface VendorListParams {
  q: string;
  state: DirectoryState;
  page: number;
}

export function useVendorList(params: MaybeRefOrGetter<VendorListParams>) {
  return useQuery({
    queryKey: computed(() =>
      queryKey("listVendors", { ...toValue(params), limit: VENDOR_PAGE_SIZE }),
    ),
    queryFn: ({ signal }) => {
      const { q, state, page } = toValue(params);
      return unwrap(
        api.GET("/admin/api/vendors", {
          params: { query: { q: q || undefined, state, page, limit: VENDOR_PAGE_SIZE } },
          signal,
        }),
      );
    },
    placeholderData: keepPreviousData,
  });
}

/** Vendor suggestions for pickers (copy target, import target). */
export function useVendorSearch(search: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() =>
      queryKey("listVendors", { q: toValue(search), state: "current", page: 1, limit: 20 }),
    ),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/vendors", {
          params: { query: { q: toValue(search) || undefined, page: 1, limit: 20 } },
          signal,
        }),
      ),
    placeholderData: keepPreviousData,
  });
}

export function useVendor(vendor: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() => vendorKey(toValue(vendor))),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/vendors/{vendor}", {
          params: { path: { vendor: toValue(vendor) } },
          signal,
        }),
      ),
  });
}

export interface AppListParams {
  vendor: string;
  q: string;
  state: DirectoryState;
  sort: AppSort;
  order: "asc" | "desc";
  lang: Schema<"Language">;
  page: number;
}

export function useAppList(params: MaybeRefOrGetter<AppListParams>) {
  return useQuery({
    queryKey: computed(() => queryKey("listApps", { ...toValue(params), limit: APP_PAGE_SIZE })),
    queryFn: ({ signal }) => {
      const { q, ...rest } = toValue(params);
      return unwrap(
        api.GET("/admin/api/apps", {
          params: { query: { ...rest, q: q || undefined, limit: APP_PAGE_SIZE } },
          signal,
        }),
      );
    },
    placeholderData: keepPreviousData,
  });
}

/**
 * Every matching application of a vendor, for the vendor card strip. The
 * vendor list previews at most five; the rest is loaded page by page only
 * when `enabled` (the preview is incomplete).
 */
export function useAllVendorApps(
  params: MaybeRefOrGetter<{ vendor: string; q: string; state: DirectoryState }>,
  enabled: MaybeRefOrGetter<boolean>,
) {
  return useQuery({
    queryKey: computed(() => queryKey("listApps", { ...toValue(params), all: true })),
    queryFn: async ({ signal }) => {
      const { vendor, q, state } = toValue(params);
      const items: AppListItem[] = [];
      for (let page = 1; ; page++) {
        const result = await unwrap(
          api.GET("/admin/api/apps", {
            params: {
              query: { vendor, q: q || undefined, state, page, limit: ALL_PAGE_SIZE },
            },
            signal,
          }),
        );
        items.push(...result.items);
        if (page >= result.total_pages || result.items.length === 0) {
          return { items, total: result.total };
        }
      }
    },
    enabled: computed(() => toValue(enabled)),
  });
}

export function useApp(vendor: MaybeRefOrGetter<string>, app: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() => appKey(toValue(vendor), toValue(app))),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/apps/{vendor}/{app}", {
          params: { path: { vendor: toValue(vendor), app: toValue(app) } },
          signal,
        }),
      ),
  });
}

/** Lists whose contents or counts follow the set of vendors and applications. */
function refreshDirectory(client: QueryClient): void {
  for (const key of [
    queryKey("listVendors"),
    queryKey("listApps"),
    queryKey("listCategories"),
    queryKey("getBootstrap"),
  ]) {
    void client.invalidateQueries({ queryKey: key });
  }
}

export function useCreateVendor() {
  const client = useQueryClient();
  return useMutation({
    meta: { handledCodes: ["ALREADY_EXISTS"] },
    mutationFn: (body: Schema<"VendorCreate">) => unwrap(api.POST("/admin/api/vendors", { body })),
    onSuccess: (vendor) => {
      client.setQueryData(vendorKey(vendor.id), vendor);
      refreshDirectory(client);
    },
  });
}

export function useCreateApp() {
  const client = useQueryClient();
  return useMutation({
    meta: { handledCodes: ["ALREADY_EXISTS"] },
    mutationFn: (body: Schema<"AppCreate">) => unwrap(api.POST("/admin/api/apps", { body })),
    onSuccess: (app) => {
      client.setQueryData(appKey(app.vendor_id, app.id), app);
      refreshDirectory(client);
    },
  });
}

/**
 * Conflicts of one-click actions (enable switch, delete) have no draft to
 * keep: the latest state is reloaded and the user is told to try again.
 */
function onActionConflict(client: QueryClient, error: unknown, keys: QueryKey[]): void {
  if (!isApiError(error, "REVISION_CONFLICT")) return;
  toast({ tone: "warning", title: translate("directory.actions.conflict") });
  for (const key of keys) void client.invalidateQueries({ queryKey: key });
  refreshDirectory(client);
}

/** Enables or disables a vendor at once (`updateVendor`, If-Match of the given revision). */
export function useVendorEnabled() {
  const client = useQueryClient();
  return useMutation({
    meta: { handledCodes: ["REVISION_CONFLICT"] },
    mutationFn: ({
      vendor,
      enabled,
    }: {
      vendor: Pick<Vendor, "id" | "revision">;
      enabled: boolean;
    }) =>
      unwrap(
        api.PATCH("/admin/api/vendors/{vendor}", {
          params: { path: { vendor: vendor.id }, header: { "If-Match": ifMatch(vendor.revision) } },
          body: { enabled },
        }),
      ),
    onSuccess: (vendor) => {
      client.setQueryData(vendorKey(vendor.id), vendor);
      // The configuration shares the vendor revision.
      void client.invalidateQueries({ queryKey: vendorConfigurationKey(vendor.id) });
      refreshDirectory(client);
    },
    onError: (error, { vendor }) => {
      onActionConflict(client, error, [vendorKey(vendor.id), vendorConfigurationKey(vendor.id)]);
    },
  });
}

type AppRef = Pick<App, "vendor_id" | "id" | "revision">;

/** Enables or disables an application at once (`updateApp`). */
export function useAppEnabled() {
  const client = useQueryClient();
  return useMutation({
    meta: { handledCodes: ["REVISION_CONFLICT"] },
    mutationFn: ({ app, enabled }: { app: AppRef; enabled: boolean }) =>
      unwrap(
        api.PATCH("/admin/api/apps/{vendor}/{app}", {
          params: {
            path: { vendor: app.vendor_id, app: app.id },
            header: { "If-Match": ifMatch(app.revision) },
          },
          body: { enabled },
        }),
      ),
    onSuccess: (app) => {
      client.setQueryData(appKey(app.vendor_id, app.id), app);
      void client.invalidateQueries({ queryKey: appConfigurationKey(app.vendor_id, app.id) });
      refreshDirectory(client);
    },
    onError: (error, { app }) => {
      onActionConflict(client, error, [
        appKey(app.vendor_id, app.id),
        appConfigurationKey(app.vendor_id, app.id),
      ]);
    },
  });
}

/** Deletes a custom vendor. A 404 means it is already gone (a retried request). */
export function useDeleteVendor() {
  const client = useQueryClient();
  return useMutation({
    meta: { handledCodes: ["REVISION_CONFLICT", "VENDOR_NOT_FOUND"] },
    mutationFn: async (vendor: Pick<Vendor, "id" | "revision">) => {
      try {
        await unwrap(
          api.DELETE("/admin/api/vendors/{vendor}", {
            params: {
              path: { vendor: vendor.id },
              header: { "If-Match": ifMatch(vendor.revision) },
            },
          }),
        );
      } catch (error) {
        if (!isApiError(error, "VENDOR_NOT_FOUND")) throw error;
      }
    },
    onSuccess: (_result, vendor) => {
      // Marked stale only: the open page navigates away instead of refetching a 404.
      void client.invalidateQueries({ queryKey: vendorKey(vendor.id), refetchType: "none" });
      refreshDirectory(client);
    },
    onError: (error, vendor) => {
      onActionConflict(client, error, [vendorKey(vendor.id)]);
    },
  });
}

/**
 * Deletes a custom application. The spec answers a retry after a successful
 * deletion with 404 APPLICATION_NOT_FOUND: that counts as done. Resolves with
 * `cleanup_pending` (files still being removed).
 */
export function useDeleteApp() {
  const client = useQueryClient();
  return useMutation({
    meta: { handledCodes: ["REVISION_CONFLICT", "APPLICATION_NOT_FOUND"] },
    mutationFn: async (app: AppRef & Pick<App, "uid">) => {
      try {
        return await unwrap(
          api.DELETE("/admin/api/apps/{vendor}/{app}", {
            params: {
              path: { vendor: app.vendor_id, app: app.id },
              query: { confirm_uid: app.uid },
              header: { "If-Match": ifMatch(app.revision) },
            },
          }),
        );
      } catch (error) {
        if (isApiError(error, "APPLICATION_NOT_FOUND")) return { cleanup_pending: false };
        throw error;
      }
    },
    onSuccess: (_result, app) => {
      // Marked stale only: the open page navigates away instead of refetching a 404.
      for (const key of [
        appKey(app.vendor_id, app.id),
        appConfigurationKey(app.vendor_id, app.id),
      ]) {
        void client.invalidateQueries({ queryKey: key, refetchType: "none" });
      }
      refreshDirectory(client);
    },
    onError: (error, app) => {
      onActionConflict(client, error, [appKey(app.vendor_id, app.id)]);
    },
  });
}

/** Uploads an icon image; resolves with the stored `IconPath`. */
export async function uploadIcon(
  file: File,
  options: { signal?: AbortSignal; onProgress?: (progress: UploadProgress) => void } = {},
): Promise<string> {
  const body = new FormData();
  body.append("file", file);
  const stored = await uploadWithProgress<Schema<"StoredIcon">>({
    url: "/admin/api/icons",
    body,
    ...options,
  });
  return stored.icon;
}
