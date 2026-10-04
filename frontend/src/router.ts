import { createRouter, createWebHistory, type RouterHistory } from "vue-router";
import { nextTick } from "vue";
import { validApplicationID } from "./bootstrap";
export function adminReturnPath(value: unknown): string {
  return typeof value === "string" &&
    value.startsWith("/admin/") &&
    !value.startsWith("/admin/login") &&
    !/[\\\r\n]/.test(value)
    ? value
    : "/admin/vendors";
}
export function makeRouter(history: RouterHistory = createWebHistory()) {
  const router = createRouter({
    history,
    routes: [
      {
        path: "/",
        component: () => import("./layouts/PublicLayout.vue"),
        children: [
          { path: "", component: () => import("./PublicApp.vue") },
          {
            path: ":vendor/:app",
            component: () => import("./PublicApp.vue"),
            beforeEnter: (to) =>
              validApplicationID(`${to.params.vendor}/${to.params.app}`) || {
                name: "not-found",
              },
          },
        ],
      },
      {
        path: "/admin",
        component: () => import("./layouts/AdminLayout.vue"),
        children: [
          { path: "", redirect: "/admin/overview" },
          { path: "login", component: () => import("./pages/LoginPage.vue") },
          {
            path: "overview",
            component: () => import("./pages/StatusPage.vue"),
          },
          {
            path: "events",
            component: () => import("./pages/EventsPage.vue"),
          },
          {
            path: "settings/site",
            component: () => import("./pages/SiteSettingsPage.vue"),
          },
          {
            path: "settings/proxy",
            component: () => import("./components/ProxySettings.vue"),
          },
          {
            path: "vendors",
            component: () => import("./pages/DirectoryPage.vue"),
          },
          {
            path: "vendors/new",
            component: () => import("./components/DirectoryEditor.vue"),
            props: { kind: "vendor" },
          },
          {
            path: "vendors/:vendor/apps/new",
            component: () => import("./components/DirectoryEditor.vue"),
            props: { kind: "app" },
          },
          {
            path: "vendors/:vendor/settings",
            component: () => import("./components/DirectoryEditor.vue"),
            props: { kind: "vendor" },
          },
          {
            path: "vendors/:vendor/apps/:app",
            component: () => import("./layouts/ApplicationLayout.vue"),
            children: [
              {
                path: "files",
                component: () => import("./pages/HostedFilesPage.vue"),
              },
              {
                path: "cache",
                component: () => import("./pages/ApplicationCachePage.vue"),
              },
              {
                path: "versions",
                component: () => import("./pages/ApplicationResourcesPage.vue"),
              },
              {
                path: "settings",
                component: () => import("./pages/ApplicationSettingsPage.vue"),
              },
            ],
          },
        ],
      },
      {
        path: "/:pathMatch(.*)*",
        name: "not-found",
        component: () => import("./pages/NotFoundPage.vue"),
      },
    ],
    scrollBehavior(_to, _from, saved) {
      return saved || { top: 0 };
    },
  });
  router.afterEach(async (_to, _from, failure) => {
    if (!failure) {
      await nextTick();
      document.getElementById("main-content")?.focus({ preventScroll: true });
    }
  });
  return router;
}
