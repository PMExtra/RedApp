import { effectScope, nextTick, watch, watchEffect, type App } from "vue";
import { createPinia } from "pinia";
import { VueQueryPlugin, type QueryClient } from "@tanstack/vue-query";
import { START_LOCATION, type Router } from "vue-router";
import { createQueryClient } from "@/shared/api";
import { createAppI18n, type Locale, type LocaleGlob } from "@/shared/i18n";
import { currentPageTitle, currentSiteTitle, notifyError, usePreferencesStore } from "@/shared/lib";

declare module "vue-router" {
  interface RouteMeta {
    /** i18n key of the page title; dynamic titles use `useDocumentTitle`. */
    titleKey?: string;
    /** Admin pages reachable without a session (the login page). */
    public?: boolean;
  }
}

export interface InstallOptions {
  router: Router;
  messages: Record<Locale, LocaleGlob>;
  /** Tests pin the locale and fail on missing keys. */
  locale?: Locale;
  strictI18n?: boolean;
  queryClient?: QueryClient;
}

/** Installs Pinia, vue-i18n, TanStack Query and the router with shell behaviour. */
export function installCore(app: App, options: InstallOptions) {
  const pinia = createPinia();
  app.use(pinia);
  const preferences = usePreferencesStore(pinia);
  if (options.locale) preferences.locale = options.locale;

  const i18n = createAppI18n({
    messages: options.messages,
    locale: preferences.locale,
    strict: options.strictI18n,
  });
  app.use(i18n);

  const queryClient = options.queryClient ?? createQueryClient({ onMutationError: notifyError });
  app.use(VueQueryPlugin, { queryClient });

  const { router } = options;
  router.afterEach((to, from, failure) => {
    // Move focus to the new page's main landmark on real page changes only, so
    // query-only updates (search boxes, filters) keep focus and selection.
    if (failure || from === START_LOCATION || to.path === from.path) return;
    void nextTick(() => document.getElementById("main-content")?.focus({ preventScroll: true }));
  });
  app.use(router);

  const scope = effectScope(true);
  scope.run(() => {
    watch(
      () => preferences.locale,
      (locale) => {
        i18n.global.locale.value = locale;
        document.documentElement.lang = locale;
      },
      { immediate: true },
    );
    watch(
      () => preferences.resolvedTheme,
      (theme) => {
        document.documentElement.dataset.theme = theme;
      },
      { immediate: true },
    );
    watchEffect(() => {
      const route = router.currentRoute.value;
      const page =
        currentPageTitle() ?? (route.meta.titleKey ? i18n.global.t(route.meta.titleKey) : null);
      document.title = [page, currentSiteTitle()].filter(Boolean).join(" · ");
    });
  });
  app.onUnmount(() => {
    scope.stop();
  });

  return { pinia, i18n, queryClient };
}

/** Scroll to the top on page changes, keep position on query-only changes. */
export function scrollBehavior(
  to: { path: string; hash: string },
  from: { path: string },
  saved: { left: number; top: number } | null,
) {
  if (saved) return saved;
  if (to.path === from.path) return false;
  return { top: 0 };
}
