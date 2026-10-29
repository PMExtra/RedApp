import { computed, type MaybeRefOrGetter, toValue } from "vue";
import { useQuery } from "@tanstack/vue-query";
import { api, queryKey, unwrap } from "@/shared/api";

/**
 * Download choices of a JSON release: the default channel's release, or the
 * discovered release `release` names. Components showing the same release
 * share one request.
 */
export function useReleaseDownloads(
  vendor: MaybeRefOrGetter<string>,
  app: MaybeRefOrGetter<string>,
  release?: MaybeRefOrGetter<string | undefined>,
) {
  return useQuery({
    queryKey: computed(() =>
      queryKey("getReleaseDownloads", {
        vendor: toValue(vendor),
        app: toValue(app),
        release: toValue(release),
      }),
    ),
    queryFn: ({ signal }) => {
      const selected = toValue(release);
      return unwrap(
        api.GET("/api/apps/{vendor}/{app}/downloads", {
          params: {
            path: { vendor: toValue(vendor), app: toValue(app) },
            query: selected ? { release: selected } : {},
          },
          signal,
        }),
      );
    },
  });
}

/**
 * Every release the application discovered, newest version first. `enabled`
 * lets a caller wait until reading the downloads refreshed the channel.
 */
export function useReleaseHistory(
  vendor: MaybeRefOrGetter<string>,
  app: MaybeRefOrGetter<string>,
  enabled: MaybeRefOrGetter<boolean> = true,
) {
  return useQuery({
    enabled: computed(() => toValue(enabled)),
    queryKey: computed(() =>
      queryKey("listReleases", { vendor: toValue(vendor), app: toValue(app) }),
    ),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/api/apps/{vendor}/{app}/releases", {
          params: { path: { vendor: toValue(vendor), app: toValue(app) } },
          signal,
        }),
      ),
  });
}

/** Packages of the current releases, for choosing download choices (admin). */
export function useReleaseArtifacts(
  vendor: MaybeRefOrGetter<string>,
  app: MaybeRefOrGetter<string>,
) {
  return useQuery({
    queryKey: computed(() =>
      queryKey("listReleaseArtifacts", { vendor: toValue(vendor), app: toValue(app) }),
    ),
    queryFn: ({ signal }) =>
      unwrap(
        api.GET("/admin/api/apps/{vendor}/{app}/artifacts", {
          params: { path: { vendor: toValue(vendor), app: toValue(app) } },
          signal,
        }),
      ),
  });
}
