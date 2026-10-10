import type { RouteRecordRaw } from "vue-router";

/**
 * Admin routes. Every named route must be listed in the spec's `x-spa-routes`
 * (checked by spaRoutes.test.ts), otherwise the server answers 404 on reload.
 * Page ownership by work package is noted per group.
 */
export const adminRoutes: RouteRecordRaw[] = [
  {
    path: "/admin",
    component: () => import("./AdminLayout.vue"),
    children: [
      { path: "", redirect: { name: "admin-overview" } },
      {
        path: "login",
        name: "admin-login",
        component: () => import("@/pages/admin/LoginPage.vue"),
        meta: { public: true, titleKey: "session.login.title" },
      },

      // Package D: overview, events, settings.
      {
        path: "overview",
        name: "admin-overview",
        component: () => import("@/pages/admin/overview/OverviewPage.vue"),
        meta: { titleKey: "adminShell.titles.overview" },
      },
      {
        path: "events",
        name: "admin-events",
        component: () => import("@/pages/admin/events/EventsPage.vue"),
        meta: { titleKey: "adminShell.titles.events" },
      },
      {
        path: "settings/site",
        name: "admin-settings-site",
        component: () => import("@/pages/admin/settings/SiteSettingsPage.vue"),
        meta: { titleKey: "adminShell.titles.site" },
      },
      {
        path: "settings/proxy",
        name: "admin-settings-proxy",
        component: () => import("@/pages/admin/settings/ProxySettingsPage.vue"),
        meta: { titleKey: "adminShell.titles.proxy" },
      },

      // Package B: directory.
      {
        path: "vendors",
        name: "admin-vendors",
        component: () => import("@/pages/admin/vendors/VendorListPage.vue"),
        meta: { titleKey: "adminShell.titles.vendors" },
      },
      {
        path: "vendors/new",
        name: "admin-vendor-new",
        component: () => import("@/pages/admin/vendors/VendorCreatePage.vue"),
        meta: { titleKey: "adminShell.titles.vendorNew" },
      },
      {
        path: "categories",
        name: "admin-categories",
        component: () => import("@/pages/admin/categories/CategoriesPage.vue"),
        meta: { titleKey: "adminShell.titles.categories" },
      },
      {
        path: "vendors/:vendor/apps/new",
        name: "admin-app-new",
        component: () => import("@/pages/admin/apps/AppCreatePage.vue"),
        meta: { titleKey: "adminShell.titles.appNew" },
      },
      {
        path: "vendors/:vendor",
        component: () => import("@/pages/admin/vendors/VendorLayout.vue"),
        children: [
          { path: "", name: "admin-vendor", redirect: { name: "admin-vendor-settings" } },
          {
            path: "settings",
            name: "admin-vendor-settings",
            component: () => import("@/pages/admin/vendors/VendorSettingsPage.vue"),
          },
          {
            path: "apps",
            name: "admin-vendor-apps",
            component: () => import("@/pages/admin/vendors/VendorAppsPage.vue"),
          },
          {
            path: "admin-notes",
            name: "admin-vendor-notes",
            component: () => import("@/pages/admin/vendors/VendorNotesPage.vue"),
          },
        ],
      },
      {
        // The tab host (package B) gates tabs by provider capabilities.
        path: "vendors/:vendor/apps/:app",
        component: () => import("@/pages/admin/apps/AppLayout.vue"),
        children: [
          { path: "", redirect: { name: "admin-app-settings" } },
          {
            path: "settings",
            name: "admin-app-settings",
            component: () => import("@/pages/admin/apps/AppSettingsPage.vue"),
          },
          {
            path: "admin-notes",
            name: "admin-app-notes",
            component: () => import("@/pages/admin/apps/AppNotesPage.vue"),
          },
          // Package C: runtime tabs.
          {
            path: "versions",
            name: "admin-app-versions",
            component: () => import("@/pages/admin/apps/AppVersionsPage.vue"),
          },
          {
            path: "cache",
            name: "admin-app-cache",
            component: () => import("@/pages/admin/apps/AppCachePage.vue"),
          },
          {
            path: "files",
            name: "admin-app-files",
            component: () => import("@/pages/admin/apps/AppFilesPage.vue"),
          },
        ],
      },

      {
        path: ":pathMatch(.*)*",
        name: "admin-not-found",
        component: () => import("@/pages/admin/NotFoundPage.vue"),
        meta: { titleKey: "adminShell.titles.notFound" },
      },
    ],
  },
];
