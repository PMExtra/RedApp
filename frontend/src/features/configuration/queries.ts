import { computed, toValue, type MaybeRefOrGetter } from "vue";
import { useQuery, useQueryClient } from "@tanstack/vue-query";
import {
  api,
  queryKey,
  unwrap,
  useRevisionedMutation,
  type AnyErrorCode,
  type Schema,
} from "@/shared/api";

/*
 * The configuration overlay of an application (or vendor) is ONE revisioned
 * resource. Every section that edits it (names, icon, proxy, instructions,
 * taxonomy, cache policy, retention, prewarm) must read it through these
 * queries and write it through these mutations, so a save by one section
 * updates the revision seen by all others instead of causing 409s.
 */

export type AppConfiguration = Schema<"AppConfiguration">;
export type AppConfigurationPatch = Schema<"AppConfigurationPatch">;
export type VendorConfiguration = Schema<"VendorConfiguration">;
export type VendorConfigurationPatch = Schema<"VendorConfigurationPatch">;

export function appConfigurationKey(vendor: string, app: string) {
  return queryKey("getAppConfiguration", { vendor, app });
}

export function vendorConfigurationKey(vendor: string) {
  return queryKey("getVendorConfiguration", { vendor });
}

export function useAppConfiguration(
  vendor: MaybeRefOrGetter<string>,
  app: MaybeRefOrGetter<string>,
) {
  return useQuery({
    queryKey: computed(() => appConfigurationKey(toValue(vendor), toValue(app))),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/apps/{vendor}/{app}/configuration", {
          params: { path: { vendor: toValue(vendor), app: toValue(app) } },
          signal,
        }),
      ),
  });
}

export function useVendorConfiguration(vendor: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() => vendorConfigurationKey(toValue(vendor))),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/vendors/{vendor}/configuration", {
          params: { path: { vendor: toValue(vendor) } },
          signal,
        }),
      ),
  });
}

export interface PatchOptions {
  /** Error codes the form shows itself (e.g. `PROXY_REDACTED_MISMATCH` on the proxy field). */
  handledCodes?: readonly AnyErrorCode[];
}

/**
 * PATCH `{set, unset, new_categories}` against the cached revision. The
 * response replaces the cache; 409 sets `conflict` (keep the draft).
 * The configuration shares its revision (ETag) with the application, so the
 * application and the directory lists are refreshed after a save.
 */
export function useAppConfigurationPatch(
  vendor: MaybeRefOrGetter<string>,
  app: MaybeRefOrGetter<string>,
  configuration: MaybeRefOrGetter<AppConfiguration | undefined>,
  options: PatchOptions = {},
) {
  const queryClient = useQueryClient();
  return useRevisionedMutation<AppConfiguration, AppConfigurationPatch>({
    handledCodes: options.handledCodes,
    revision: () => toValue(configuration)?.revision,
    queryKey: () => appConfigurationKey(toValue(vendor), toValue(app)),
    onSuccess: () => {
      const params = { vendor: toValue(vendor), app: toValue(app) };
      for (const key of [
        queryKey("getApp", params),
        queryKey("listApps"),
        queryKey("listVendors"),
        queryKey("listCategories"),
      ]) {
        void queryClient.invalidateQueries({ queryKey: key });
      }
    },
    mutationFn: (body, ifMatch) =>
      unwrap(
        api.PATCH("/admin/api/apps/{vendor}/{app}/configuration", {
          params: {
            path: { vendor: toValue(vendor), app: toValue(app) },
            header: { "If-Match": ifMatch },
          },
          body,
        }),
      ),
  });
}

export function useVendorConfigurationPatch(
  vendor: MaybeRefOrGetter<string>,
  configuration: MaybeRefOrGetter<VendorConfiguration | undefined>,
  options: PatchOptions = {},
) {
  const queryClient = useQueryClient();
  return useRevisionedMutation<VendorConfiguration, VendorConfigurationPatch>({
    handledCodes: options.handledCodes,
    revision: () => toValue(configuration)?.revision,
    queryKey: () => vendorConfigurationKey(toValue(vendor)),
    // Shares its revision with the vendor, like the application configuration.
    onSuccess: () => {
      const keys = [queryKey("getVendor", { vendor: toValue(vendor) }), queryKey("listVendors")];
      for (const key of keys) {
        void queryClient.invalidateQueries({ queryKey: key });
      }
    },
    mutationFn: (body, ifMatch) =>
      unwrap(
        api.PATCH("/admin/api/vendors/{vendor}/configuration", {
          params: { path: { vendor: toValue(vendor) }, header: { "If-Match": ifMatch } },
          body,
        }),
      ),
  });
}
