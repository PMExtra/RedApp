import { computed, toValue, type MaybeRefOrGetter } from "vue";
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/vue-query";
import { appConfigurationKey } from "@/features/configuration";
import { appKey } from "@/features/directory";
import {
  api,
  ifMatch,
  isApiError,
  queryKey,
  unwrap,
  useRevisionedMutation,
  type Schema,
} from "@/shared/api";
import { translate } from "@/shared/i18n";
import { toast } from "@/shared/lib";

export type Upstream = Schema<"Upstream">;
export type UpstreamRequest = Schema<"UpstreamRequest">;
export type UpstreamCatalogItem = Schema<"UpstreamCatalogPage">["items"][number];
export type AppMirror = Schema<"AppMirror">;
export type MirrorRequest = Schema<"MirrorRequest">;

/** Page size of an upstream's catalog. */
export const UPSTREAM_CATALOG_PAGE_SIZE = 25;

export function upstreamKey(upstream: string) {
  return queryKey("getUpstream", { upstream });
}

export function appMirrorKey(vendor: string, app: string) {
  return queryKey("getAppMirror", { vendor, app });
}

/** Lists whose contents follow the set of upstreams and mirrors. */
function refreshMirrors(client: QueryClient): void {
  for (const key of [
    queryKey("listUpstreams"),
    queryKey("getUpstream"),
    queryKey("listUpstreamCatalog"),
    queryKey("listVendors"),
    queryKey("listApps"),
    queryKey("listCategories"),
    queryKey("getBootstrap"),
  ]) {
    void client.invalidateQueries({ queryKey: key });
  }
}

export function useUpstreams() {
  return useQuery({
    queryKey: queryKey("listUpstreams"),
    queryFn: ({ signal }) => unwrap(api.GET("/admin/api/upstreams", { signal })),
  });
}

export function useUpstream(
  upstream: MaybeRefOrGetter<string>,
  enabled: MaybeRefOrGetter<boolean> = true,
) {
  return useQuery({
    queryKey: computed(() => upstreamKey(toValue(upstream))),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/upstreams/{upstream}", {
          params: { path: { upstream: toValue(upstream) } },
          signal,
        }),
      ),
    enabled: computed(() => toValue(enabled) && toValue(upstream) !== ""),
  });
}

/** Errors of saving an upstream that the upstream form shows itself (no toast). */
export const UPSTREAM_FORM_CODES = [
  "ALREADY_EXISTS",
  "VALIDATION_FAILED",
  "UPSTREAM_UNAVAILABLE",
  "UPSTREAM_INCOMPATIBLE",
  "LOOP_DETECTED",
] as const;

export function useCreateUpstream() {
  const client = useQueryClient();
  return useMutation({
    meta: { handledCodes: UPSTREAM_FORM_CODES },
    mutationFn: (body: UpstreamRequest) => unwrap(api.POST("/admin/api/upstreams", { body })),
    onSuccess: (upstream) => {
      client.setQueryData(upstreamKey(upstream.uid), upstream);
      void client.invalidateQueries({ queryKey: queryKey("listUpstreams") });
    },
  });
}

/** Renames or moves an upstream; 409 keeps the draft. */
export function useUpdateUpstream(
  upstream: MaybeRefOrGetter<string>,
  current: MaybeRefOrGetter<Upstream | undefined>,
) {
  const client = useQueryClient();
  return useRevisionedMutation<Upstream, UpstreamRequest>({
    handledCodes: UPSTREAM_FORM_CODES,
    revision: () => toValue(current)?.revision,
    queryKey: () => upstreamKey(toValue(upstream)),
    onSuccess: () => {
      // A new address changes the source of every mirror of the upstream.
      refreshMirrors(client);
      void client.invalidateQueries({ queryKey: queryKey("getAppMirror") });
    },
    mutationFn: (body, ifMatch) =>
      unwrap(
        api.PATCH("/admin/api/upstreams/{upstream}", {
          params: { path: { upstream: toValue(upstream) }, header: { "If-Match": ifMatch } },
          body,
        }),
      ),
  });
}

/**
 * Deletes an upstream. A 404 means it is already gone (a retried request);
 * `UPSTREAM_IN_USE` and conflicts are reported by the caller.
 */
export function useDeleteUpstream() {
  const client = useQueryClient();
  return useMutation({
    meta: { handledCodes: ["UPSTREAM_IN_USE", "REVISION_CONFLICT", "UPSTREAM_NOT_FOUND"] },
    mutationFn: async (upstream: Pick<Upstream, "uid" | "revision">) => {
      try {
        await unwrap(
          api.DELETE("/admin/api/upstreams/{upstream}", {
            params: {
              path: { upstream: upstream.uid },
              header: { "If-Match": ifMatch(upstream.revision) },
            },
          }),
        );
      } catch (error) {
        if (!isApiError(error, "UPSTREAM_NOT_FOUND")) throw error;
      }
    },
    onSuccess: (_result, upstream) => {
      client.removeQueries({ queryKey: upstreamKey(upstream.uid) });
      void client.invalidateQueries({ queryKey: queryKey("listUpstreams") });
    },
    onError: () => {
      // The mirror count or the revision changed: show the latest state.
      void client.invalidateQueries({ queryKey: queryKey("listUpstreams") });
    },
  });
}

export function useUpstreamCatalog(
  upstream: MaybeRefOrGetter<string>,
  params: MaybeRefOrGetter<{ q: string; page: number }>,
) {
  return useQuery({
    queryKey: computed(() =>
      queryKey("listUpstreamCatalog", {
        upstream: toValue(upstream),
        ...toValue(params),
        limit: UPSTREAM_CATALOG_PAGE_SIZE,
      }),
    ),
    queryFn: ({ signal }) => {
      const { q, page } = toValue(params);
      return unwrap(
        api.GET("/admin/api/upstreams/{upstream}/catalog", {
          params: {
            path: { upstream: toValue(upstream) },
            query: { q: q || undefined, page, limit: UPSTREAM_CATALOG_PAGE_SIZE },
          },
          signal,
        }),
      );
    },
    placeholderData: keepPreviousData,
  });
}

/**
 * The local application `vendor/app`, or `null` when it does not exist. Used
 * to decide between creating a mirror and adopting an existing application.
 */
export function useLocalApp(
  vendor: MaybeRefOrGetter<string>,
  app: MaybeRefOrGetter<string>,
  enabled: MaybeRefOrGetter<boolean>,
) {
  return useQuery({
    // Shares the prefix of `getApp`, so the directory's invalidations refresh it.
    queryKey: computed(() => [...appKey(toValue(vendor), toValue(app)), "lookup"] as const),
    queryFn: async ({ signal }) => {
      try {
        return await unwrap(
          api.GET("/admin/api/apps/{vendor}/{app}", {
            params: { path: { vendor: toValue(vendor), app: toValue(app) } },
            signal,
          }),
        );
      } catch (error) {
        if (isApiError(error, "APPLICATION_NOT_FOUND") || isApiError(error, "VENDOR_NOT_FOUND")) {
          return null;
        }
        throw error;
      }
    },
    enabled: computed(() => toValue(enabled)),
  });
}

/** Reads the local application `vendor/app` again (its lookup shares the `getApp` key). */
export function useRecheckTarget() {
  const client = useQueryClient();
  return (vendor: string, app: string) =>
    client.invalidateQueries({ queryKey: appKey(vendor, app) });
}

/** Mirrors one upstream application. Errors are shown per row by the caller. */
export function useCreateMirror(upstream: MaybeRefOrGetter<string>) {
  return useMutation({
    meta: { silent: true },
    mutationFn: (body: MirrorRequest) =>
      unwrap(
        api.POST("/admin/api/upstreams/{upstream}/mirrors", {
          params: { path: { upstream: toValue(upstream) } },
          body,
        }),
      ),
  });
}

/** Refreshes the catalog, the upstreams and the directory after mirroring. */
export function useRefreshAfterMirroring() {
  const client = useQueryClient();
  return (apps: Schema<"App">[]) => {
    for (const app of apps) {
      client.setQueryData(appKey(app.vendor_id, app.id), app);
      void client.invalidateQueries({ queryKey: appConfigurationKey(app.vendor_id, app.id) });
      void client.invalidateQueries({ queryKey: appMirrorKey(app.vendor_id, app.id) });
    }
    refreshMirrors(client);
  };
}

export function useAppMirror(
  vendor: MaybeRefOrGetter<string>,
  app: MaybeRefOrGetter<string>,
  enabled: MaybeRefOrGetter<boolean>,
) {
  return useQuery({
    queryKey: computed(() => appMirrorKey(toValue(vendor), toValue(app))),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/apps/{vendor}/{app}/mirror", {
          params: { path: { vendor: toValue(vendor), app: toValue(app) } },
          signal,
        }),
      ),
    enabled: computed(() => toValue(enabled)),
  });
}

/** Refreshes what a check or an unbind may change for `vendor/app`. */
function refreshMirroredApp(client: QueryClient, vendor: string, app: string): void {
  for (const key of [appKey(vendor, app), appConfigurationKey(vendor, app)]) {
    void client.invalidateQueries({ queryKey: key });
  }
  for (const key of [queryKey("listApps"), queryKey("listVendors")]) {
    void client.invalidateQueries({ queryKey: key });
  }
}

/** Checks the upstream application now. */
export function useSyncAppMirror(vendor: MaybeRefOrGetter<string>, app: MaybeRefOrGetter<string>) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/admin/api/apps/{vendor}/{app}/mirror/sync", {
          params: { path: { vendor: toValue(vendor), app: toValue(app) } },
        }),
      ),
    onSuccess: (mirror) => {
      const [v, a] = [toValue(vendor), toValue(app)];
      client.setQueryData(appMirrorKey(v, a), mirror);
      refreshMirroredApp(client, v, a);
    },
  });
}

/**
 * Stops mirroring (If-Match: the application revision). The application keeps
 * its current values. A conflict has no draft to keep: the latest state is
 * reloaded and the user is told to try again.
 */
export function useUnbindAppMirror() {
  const client = useQueryClient();
  return useMutation({
    meta: { handledCodes: ["REVISION_CONFLICT"] },
    mutationFn: (target: Pick<Schema<"App">, "vendor_id" | "id" | "revision">) =>
      unwrap(
        api.DELETE("/admin/api/apps/{vendor}/{app}/mirror", {
          params: {
            path: { vendor: target.vendor_id, app: target.id },
            header: { "If-Match": ifMatch(target.revision) },
          },
        }),
      ),
    onSuccess: (updated) => {
      client.setQueryData(appKey(updated.vendor_id, updated.id), updated);
      client.removeQueries({ queryKey: appMirrorKey(updated.vendor_id, updated.id) });
      refreshMirroredApp(client, updated.vendor_id, updated.id);
      refreshMirrors(client);
    },
    onError: (error, target) => {
      if (!isApiError(error, "REVISION_CONFLICT")) return;
      toast({ tone: "warning", title: translate("mirrors.section.conflict") });
      refreshMirroredApp(client, target.vendor_id, target.id);
      void client.invalidateQueries({ queryKey: appMirrorKey(target.vendor_id, target.id) });
    },
  });
}
