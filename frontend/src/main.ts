import { createApp } from "vue";
import "./style.css";
const page = window.location.pathname.startsWith("/admin")
  ? import("./App.vue")
  : import("./PublicApp.vue");
void page.then(({ default: App }) => createApp(App).mount("#app"));
