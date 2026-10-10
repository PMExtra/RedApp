import "@/shared/styles/main.css";
import { createApp } from "vue";
import { createRouter, createWebHistory } from "vue-router";
import AppRoot from "@/app/core/AppRoot.vue";
import { installCore, scrollBehavior } from "@/app/core/install";
import { installParamGuard } from "@/app/core/paramGuard";
import { publicMessages } from "./messages";
import { publicRoutes } from "./routes";

const router = createRouter({ history: createWebHistory(), routes: publicRoutes, scrollBehavior });
installParamGuard(router, "public-not-found");
const app = createApp(AppRoot);
installCore(app, { router, messages: publicMessages });
app.mount("#app");
