import { defineComponent, h, type Component } from "vue";
import { render, type RenderResult } from "@testing-library/vue";
import { createMemoryHistory, createRouter, type RouteRecordRaw, type Router } from "vue-router";
import type { QueryClient } from "@tanstack/vue-query";
import AppRoot from "@/app/core/AppRoot.vue";
import { installCore } from "@/app/core/install";
import { installParamGuard } from "@/app/core/paramGuard";
import { adminMessages } from "@/app/admin/messages";
import { installSessionGuard } from "@/app/admin/guard";
import { adminRoutes } from "@/app/admin/routes";
import { publicMessages } from "@/app/public/messages";
import { publicRoutes } from "@/app/public/routes";
import { useSessionStore } from "@/features/session";
import { configureApi, createQueryClient } from "@/shared/api";
import { notifyError } from "@/shared/lib";
import { ConfirmHost, Toaster, TooltipProvider } from "@/shared/ui";
import type { Locale } from "@/shared/i18n";

export interface Rendered extends RenderResult {
  router: Router;
  queryClient: QueryClient;
}

function testQueryClient() {
  const client = createQueryClient({ onMutationError: notifyError });
  client.setDefaultOptions({
    ...client.getDefaultOptions(),
    queries: { ...client.getDefaultOptions().queries, retry: false },
  });
  return client;
}

/**
 * Renders a component with the app's plugins (Pinia, strict i18n, Query, a
 * memory router), inside the same providers as AppRoot (tooltips, toasts,
 * confirm dialogs). Use for shared UI and feature components.
 */
export async function renderWithApp(
  component: Component,
  options: {
    props?: Record<string, unknown>;
    slots?: Record<string, () => unknown>;
    path?: string;
    routes?: RouteRecordRaw[];
    locale?: Locale;
  } = {},
): Promise<Rendered> {
  const host = defineComponent({
    setup: () => () =>
      h(TooltipProvider, null, () => [
        h(component, options.props ?? {}, options.slots ?? {}),
        h(Toaster),
        h(ConfirmHost),
      ]),
  });
  const router = createRouter({
    history: createMemoryHistory(),
    routes: options.routes ?? [{ path: "/:pathMatch(.*)*", component: { render: () => null } }],
  });
  const queryClient = testQueryClient();
  await router.push(options.path ?? "/");
  const result = render(host, {
    global: {
      plugins: [
        {
          install: (app) => {
            installCore(app, {
              router,
              messages: adminMessages,
              locale: options.locale ?? "en",
              strictI18n: true,
              queryClient,
            });
          },
        },
      ],
    },
  });
  await router.isReady();
  return { ...result, router, queryClient };
}

/** Renders a whole entry (layout, routes, guards) at `path`, like the browser would. */
export async function renderEntry(
  entry: "public" | "admin",
  path: string,
  options: { locale?: Locale } = {},
): Promise<Rendered> {
  // Start at `path` so the initial navigation (on install) runs the guards there.
  const history = createMemoryHistory();
  history.replace(path);
  const router = createRouter({ history, routes: entry === "admin" ? adminRoutes : publicRoutes });
  installParamGuard(router, entry === "admin" ? "admin-not-found" : "public-not-found");
  if (entry === "admin") installSessionGuard(router);
  const queryClient = testQueryClient();
  const result = render(AppRoot, {
    global: {
      plugins: [
        {
          install: (app) => {
            installCore(app, {
              router,
              messages: entry === "admin" ? adminMessages : publicMessages,
              locale: options.locale ?? "en",
              strictI18n: true,
              queryClient,
            });
            if (entry === "admin") {
              configureApi({
                csrfToken: () => useSessionStore().csrfToken,
                onUnauthorized: () => {
                  useSessionStore().expire();
                },
              });
            }
          },
        },
      ],
    },
  });
  await router.isReady();
  return { ...result, router, queryClient };
}
