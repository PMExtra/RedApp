import { onScopeDispose, ref, toValue, watchEffect, type MaybeRefOrGetter } from "vue";

const pageTitle = ref<string | null>(null);
const siteTitle = ref("RedApp");

/**
 * Sets a dynamic page title (for example an application name) while the
 * calling component is mounted. Static titles use the route's `meta.titleKey`.
 * The shell appends the site title: "Codex CLI · My Mirror".
 */
export function useDocumentTitle(title: MaybeRefOrGetter<string | null | undefined>): void {
  watchEffect(() => {
    pageTitle.value = toValue(title) || null;
  });
  onScopeDispose(() => {
    pageTitle.value = null;
  });
}

/** Set by the layouts from the bootstrap site texts. */
export function useSiteTitle(title: MaybeRefOrGetter<string | null | undefined>): void {
  watchEffect(() => {
    siteTitle.value = toValue(title) || "RedApp";
  });
}

/** Read by the app shell. */
export function currentPageTitle(): string | null {
  return pageTitle.value;
}

export function currentSiteTitle(): string {
  return siteTitle.value;
}
