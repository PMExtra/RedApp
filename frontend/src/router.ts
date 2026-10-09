import { createRouter, createWebHistory, type RouterHistory } from "vue-router";
import { nextTick } from "vue";
import { validApplicationID } from "./bootstrap";
export function adminReturnPath(value: unknown): string {
  const fallback = "/admin/vendors";
  if (typeof value !== "string" || !value.startsWith("/admin/") || /[\\\r\n]/.test(value)) return fallback;
  try {
    const url = new URL(value, "https://redapp.invalid");
    const path = decodeURIComponent(url.pathname);
    if (url.origin !== "https://redapp.invalid" || !path.startsWith("/admin/") || path.startsWith("/admin/login") || /[\\\x00-\x20]/.test(path)) return fallback;
    return url.pathname + url.search + url.hash;
  } catch { return fallback; }
}
export function makeRouter(history: RouterHistory = createWebHistory()) {
  const router = createRouter({
    history,
    routes: [
      {
        path: "/",
        component: () => import("./layouts/PublicLayout.vue"),
        children: [
          { path: "", component: () => import("./pages/PublicHome.vue") },
          { path: "all", component: () => import("./pages/PublicCatalog.vue") },
          {
            path: ":vendor",
            component: () => import("./pages/PublicCatalog.vue"),
          },
          {
            path: ":vendor/:app",
            name: "public-application",
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
          {path:"categories",component:()=>import("./pages/CategoriesPage.vue")},
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
            path: "vendors/:vendor",
            component: () => import("./layouts/VendorLayout.vue"),
            children: [
              { path: "", redirect: (to) => `/admin/vendors/${to.params.vendor}/settings` },
              { path: "settings", component: () => import("./pages/VendorSettingsPage.vue") },
              { path: "apps", component: () => import("./pages/VendorApplicationsPage.vue") },
              { path: "admin-notes", component: () => import("./pages/VendorAdminNotesPage.vue") },
            ],
          },
          {
            path: "vendors/:vendor/apps/:app",
            component: () => import("./layouts/ApplicationLayout.vue"),
            children: [
              { path: "admin-notes", component: () => import("./pages/ApplicationAdminNotesPage.vue") },
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
    scrollBehavior(to, from, saved) {
      return to.path === from.path ? false : saved || { top: 0 };
    },
  });
  router.afterEach(async (to, from, failure) => {
    if (!failure && to.path !== from.path) {
      await nextTick();
      document.getElementById("main-content")?.focus({ preventScroll: true });
    }
  });
  return router;
}
