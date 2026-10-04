import { defineConfig } from "vitest/config";
import vue from "@vitejs/plugin-vue";
export default defineConfig({
  plugins: [vue()],
  base: "/",
  build: { outDir: "../internal/httpserver/web", emptyOutDir: true },
  test: {
    environment: "happy-dom",
    environmentOptions: {
      happyDOM: {
        settings: { navigation: { disableChildFrameNavigation: true } },
      },
    },
  },
});
