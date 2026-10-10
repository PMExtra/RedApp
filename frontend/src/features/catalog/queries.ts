import { computed, toValue, type MaybeRefOrGetter } from "vue";
import { useQuery } from "@tanstack/vue-query";
import { api, queryKey, unwrap, type Schema } from "@/shared/api";
import { adminAppPath } from "./links";

/** Applications per catalog page. */
export const CATALOG_PAGE_SIZE = 24;
/** Hosted files per page on the application page. */
export const HOSTED_PAGE_SIZE = 25;

/** Pinned applications and the download ranking (shared with the header search). */
export function useHome() {
  return useQuery({
    queryKey: queryKey("getHome"),
    queryFn: ({ signal }) => unwrap(api.GET("/api/home", { signal })),
  });
}

export interface CatalogFilters {
  q: string;
  vendor: string;
  category: string;
  page: number;
}

/**
 * One catalog page. Page changes keep the previous page on screen while
 * loading; a different vendor never shows the previous vendor's applications.
 */
export function useCatalog(filters: MaybeRefOrGetter<CatalogFilters>) {
  const params = computed(() => {
    const { q, vendor, category, page } = toValue(filters);
    return {
      ...(q ? { q } : {}),
      ...(vendor ? { vendor } : {}),
      ...(category ? { category } : {}),
      page,
      limit: CATALOG_PAGE_SIZE,
    };
  });
  return useQuery({
    queryKey: computed(() => queryKey("listCatalog", params.value)),
    queryFn: ({ signal }) =>
      unwrap(api.GET("/api/catalog", { params: { query: params.value }, signal })),
    placeholderData: (previous, previousQuery) => {
      const before = previousQuery?.queryKey[1] as { vendor?: string } | undefined;
      return before?.vendor === params.value.vendor ? previous : undefined;
    },
  });
}

/** A published vendor; disabled for the full catalog (`vendor` empty). */
export function usePublicVendor(vendor: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() => queryKey("getPublicVendor", { vendor: toValue(vendor) })),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/api/vendors/{vendor}", {
          params: { path: { vendor: toValue(vendor) } },
          signal,
        }),
      ),
    enabled: computed(() => toValue(vendor) !== ""),
  });
}

/** A published application; disabled while either segment is empty. */
export function usePublicApp(vendor: MaybeRefOrGetter<string>, app: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() =>
      queryKey("getPublicApp", { vendor: toValue(vendor), app: toValue(app) }),
    ),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/api/apps/{vendor}/{app}", {
          params: { path: { vendor: toValue(vendor), app: toValue(app) } },
          signal,
        }),
      ),
    enabled: computed(() => toValue(vendor) !== "" && toValue(app) !== ""),
  });
}

/** One page of a hosted application's files. */
export function usePublicHostedFiles(
  app: MaybeRefOrGetter<Pick<Schema<"PublicApp">, "key">>,
  page: MaybeRefOrGetter<number>,
) {
  const params = computed(() => {
    const [vendor = "", id = ""] = toValue(app).key.split("/");
    return { vendor, app: id, page: toValue(page) };
  });
  return useQuery({
    queryKey: computed(() => queryKey("listPublicHostedFiles", params.value)),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/api/apps/{vendor}/{app}/files", {
          params: {
            path: { vendor: params.value.vendor, app: params.value.app },
            query: { page: params.value.page, limit: HOSTED_PAGE_SIZE },
          },
          signal,
        }),
      ),
    placeholderData: (previous, previousQuery) => {
      const before = previousQuery?.queryKey[1] as { vendor?: string; app?: string } | undefined;
      return before?.vendor === params.value.vendor && before.app === params.value.app
        ? previous
        : undefined;
    },
  });
}

/**
 * Admin page for the header's administration link: the application's main
 * admin tab on application pages (`target` set), else the overview.
 */
export function useAdminLink(target: MaybeRefOrGetter<{ vendor: string; app: string } | null>) {
  const app = usePublicApp(
    () => toValue(target)?.vendor ?? "",
    () => toValue(target)?.app ?? "",
  );
  return computed(() => {
    const current = toValue(target);
    const data = app.data.value;
    if (!current || !data) return "/admin/overview";
    return adminAppPath(current.vendor, current.app, data.capabilities);
  });
}
