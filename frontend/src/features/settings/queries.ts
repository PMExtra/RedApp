import { computed, toValue, type MaybeRefOrGetter } from "vue";
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/vue-query";
import { bootstrapQueryKey } from "@/features/bootstrap";
import { api, queryKey, unwrap, useRevisionedMutation, type Schema } from "@/shared/api";

export type SiteSettings = Schema<"SiteSettings">;
export type SiteSettingsState = Schema<"SiteSettingsState">;
export type PublicUrlState = Schema<"PublicUrlState">;
export type HomepageSettingsState = Schema<"HomepageSettingsState">;
export type GlobalProxySettings = Schema<"GlobalProxySettings">;
export type GlobalProxyState = Schema<"GlobalProxyState">;
export type AppListItem = Schema<"AppListItem">;

export const siteSettingsKey = queryKey("getSiteSettings");
export const publicUrlSettingsKey = queryKey("getPublicUrlSettings");
export const homepageSettingsKey = queryKey("getHomepageSettings");
export const globalProxySettingsKey = queryKey("getGlobalProxySettings");

/*
 * Each settings document is its own revisioned resource: reads go through
 * these queries, writes through the matching `useSave…` mutation, which
 * replaces the cache with the saved state and reports 409 as `conflict`.
 */

export function useSiteSettings() {
  return useQuery({
    queryKey: siteSettingsKey,
    queryFn: ({ signal }) => unwrap(api.GET("/admin/api/settings/site", { signal })),
  });
}

/** Saving refreshes `getBootstrap`, so the shell's title and footer follow at once. */
export function useSaveSiteSettings(state: MaybeRefOrGetter<SiteSettingsState | undefined>) {
  const queryClient = useQueryClient();
  return useRevisionedMutation<SiteSettingsState, SiteSettings>({
    revision: () => toValue(state)?.revision,
    queryKey: siteSettingsKey,
    mutationFn: (body, ifMatch) =>
      unwrap(
        api.PUT("/admin/api/settings/site", { params: { header: { "If-Match": ifMatch } }, body }),
      ),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: bootstrapQueryKey }),
  });
}

export function usePublicUrlSettings() {
  return useQuery({
    queryKey: publicUrlSettingsKey,
    queryFn: ({ signal }) => unwrap(api.GET("/admin/api/settings/public-url", { signal })),
  });
}

export function useSavePublicUrl(state: MaybeRefOrGetter<PublicUrlState | undefined>) {
  const queryClient = useQueryClient();
  return useRevisionedMutation<PublicUrlState, string | null>({
    revision: () => toValue(state)?.revision,
    queryKey: publicUrlSettingsKey,
    mutationFn: (overrideUrl, ifMatch) =>
      unwrap(
        api.PUT("/admin/api/settings/public-url", {
          params: { header: { "If-Match": ifMatch } },
          body: { override_url: overrideUrl },
        }),
      ),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: bootstrapQueryKey }),
  });
}

export function useHomepageSettings() {
  return useQuery({
    queryKey: homepageSettingsKey,
    queryFn: ({ signal }) => unwrap(api.GET("/admin/api/settings/homepage", { signal })),
  });
}

export function useSaveHomepage(state: MaybeRefOrGetter<HomepageSettingsState | undefined>) {
  const queryClient = useQueryClient();
  return useRevisionedMutation<HomepageSettingsState, string[]>({
    revision: () => toValue(state)?.revision,
    queryKey: homepageSettingsKey,
    mutationFn: (keys, ifMatch) =>
      unwrap(
        api.PUT("/admin/api/settings/homepage", {
          params: { header: { "If-Match": ifMatch } },
          body: { pinned_app_keys: keys },
        }),
      ),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: queryKey("getHome") }),
  });
}

export function useGlobalProxySettings() {
  return useQuery({
    queryKey: globalProxySettingsKey,
    queryFn: ({ signal }) => unwrap(api.GET("/admin/api/settings/proxy", { signal })),
  });
}

/** `PROXY_REDACTED_MISMATCH` is shown at the URL field instead of a toast. */
export function useSaveGlobalProxy(state: MaybeRefOrGetter<GlobalProxyState | undefined>) {
  return useRevisionedMutation<GlobalProxyState, GlobalProxySettings>({
    revision: () => toValue(state)?.revision,
    queryKey: globalProxySettingsKey,
    handledCodes: ["PROXY_REDACTED_MISMATCH"],
    mutationFn: (body, ifMatch) =>
      unwrap(
        api.PUT("/admin/api/settings/proxy", { params: { header: { "If-Match": ifMatch } }, body }),
      ),
  });
}

export const APP_SEARCH_LIMIT = 8;

/** Application suggestions for pickers (`listApps`, not deleted, any vendor). */
export function useAppSearch(search: MaybeRefOrGetter<string>, lang: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() =>
      queryKey("listApps", {
        q: toValue(search).trim(),
        limit: APP_SEARCH_LIMIT,
        lang: toValue(lang),
      }),
    ),
    queryFn: ({ signal }) => {
      const q = toValue(search).trim();
      const language = toValue(lang) === "zh-CN" ? "zh-CN" : "en";
      return unwrap(
        api.GET("/admin/api/apps", {
          params: {
            query: {
              ...(q ? { q } : {}),
              limit: APP_SEARCH_LIMIT,
              lang: language,
              state: "current",
            },
          },
          signal,
        }),
      );
    },
    placeholderData: keepPreviousData,
  });
}
