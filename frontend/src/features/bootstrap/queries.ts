import { computed } from "vue";
import { queryOptions, useQuery } from "@tanstack/vue-query";
import { api, queryKey, unwrap } from "@/shared/api";
import { useLocalized } from "@/shared/i18n";

/** Invalidate after saving site settings or the public URL. */
export const bootstrapQueryKey = queryKey("getBootstrap");

export const bootstrapQuery = queryOptions({
  queryKey: bootstrapQueryKey,
  queryFn: ({ signal }) => unwrap(api.GET("/api/bootstrap", { signal })),
  // The spec: loaded once per page load and again when the tab becomes visible.
  staleTime: Number.POSITIVE_INFINITY,
  refetchOnWindowFocus: "always",
});

export function useBootstrap() {
  return useQuery(bootstrapQuery);
}

/** Site texts in the current language, with RedApp defaults until loaded. */
export function useSiteTexts() {
  const bootstrap = useBootstrap();
  const localized = useLocalized();
  const site = computed(() => bootstrap.data.value?.site);
  return {
    bootstrap,
    title: computed(() => localized(site.value?.title) || "RedApp"),
    subtitle: computed(() => localized(site.value?.subtitle)),
    disclaimer: computed(() => localized(site.value?.disclaimer)),
  };
}
