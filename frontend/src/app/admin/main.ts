import "@/shared/styles/main.css";
import { createApp } from "vue";
import { createRouter, createWebHistory } from "vue-router";
import AppRoot from "@/app/core/AppRoot.vue";
import { installCore, scrollBehavior } from "@/app/core/install";
import { installParamGuard } from "@/app/core/paramGuard";
import { useSessionStore } from "@/features/session";
import { configureApi } from "@/shared/api";
import { installSessionGuard } from "./guard";
import { adminMessages } from "./messages";
import { adminRoutes } from "./routes";

const router = createRouter({ history: createWebHistory(), routes: adminRoutes, scrollBehavior });
installParamGuard(router, "admin-not-found");
installSessionGuard(router);
const app = createApp(AppRoot);
installCore(app, { router, messages: adminMessages });
configureApi({
  csrfToken: () => useSessionStore().csrfToken,
  onUnauthorized: () => {
    useSessionStore().expire();
  },
});
app.mount("#app");
