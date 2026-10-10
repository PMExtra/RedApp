import { defineComponent, h, type Component } from "vue";
import { RouterView } from "vue-router";
import { renderWithApp, type Rendered } from "./render";

const routerView = defineComponent({ render: () => h(RouterView) });

/**
 * Renders a page or panel as the matched route component of
 * `/admin/vendors/<vendor>/apps/<app>/<tab>`, so `route.params` is set and
 * route guards (unsaved-changes prompts) work as in the app.
 */
export function renderAppPage(
  component: Component,
  options: { key?: string; tab?: string; props?: Record<string, unknown> } = {},
): Promise<Rendered> {
  const [vendor = "openai", app = "codex"] = (options.key ?? "openai/codex").split("/");
  return renderWithApp(routerView, {
    routes: [
      {
        path: "/admin/vendors/:vendor/apps/:app/:tab",
        component,
        props: options.props ?? false,
      },
      { path: "/:pathMatch(.*)*", component: { render: () => null } },
    ],
    path: `/admin/vendors/${vendor}/apps/${app}/${options.tab ?? "cache"}`,
  });
}
