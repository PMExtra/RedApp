import { createRouter, createWebHistory, type RouterHistory } from "vue-router";
import { nextTick } from "vue";
import { validApplicationID } from "./bootstrap";
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
            props: { kind: "overview" },
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
            path: "apps/:vendor/:app",
            component: () => import("./layouts/ApplicationLayout.vue"),
            children: [
              {
                path: "versions",
                component: () => import("./pages/StatusPage.vue"),
                props: { kind: "versions" },
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
