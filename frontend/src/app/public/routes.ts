import type { RouteRecordRaw } from "vue-router";

/**
 * Public routes. Every path here must be listed in the spec's `x-spa-routes`
 * (checked by spaRoutes.test.ts), otherwise the server answers 404.
 */
export const publicRoutes: RouteRecordRaw[] = [
  {
    path: "/",
    component: () => import("./PublicLayout.vue"),
    children: [
      {
        path: "",
        name: "public-home",
        component: () => import("@/pages/public/HomePage.vue"),
      },
      {
        path: "all",
        name: "public-catalog",
        component: () => import("@/pages/public/CatalogPage.vue"),
        meta: { titleKey: "publicShell.titles.catalog" },
      },
      {
        path: ":vendor",
        name: "public-vendor",
        component: () => import("@/pages/public/CatalogPage.vue"),
      },
      {
        path: ":vendor/:app",
        name: "public-app",
        component: () => import("@/pages/public/AppPage.vue"),
      },
      {
        path: ":pathMatch(.*)*",
        name: "public-not-found",
        component: () => import("@/pages/public/NotFoundPage.vue"),
        meta: { titleKey: "publicShell.titles.notFound" },
      },
    ],
  },
];
