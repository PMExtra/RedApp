import { readFileSync } from "node:fs";
import { fileURLToPath, URL } from "node:url";
import type { Plugin, Connect } from "vite";
import { defineConfig } from "vitest/config";
import vue from "@vitejs/plugin-vue";
import tailwindcss from "@tailwindcss/vite";

const src = fileURLToPath(new URL("./src", import.meta.url));

// The Go server serves admin.html for /admin/... and index.html for public
// pages (see docs/dev/frontend.md). The dev server mirrors that split.
function adminDocument(): Plugin {
  const rewrite: Connect.NextHandleFunction = (request, _response, next) => {
    const url = request.url ?? "";
    if (/^\/admin(?:\/(?!api\/)[^?]*)?(?:\?.*)?$/.test(url)) {
      request.url = "/admin.html";
    }
    next();
  };
  return {
    name: "redapp-admin-document",
    configureServer: (server) => void server.middlewares.use(rewrite),
    configurePreviewServer: (server) => void server.middlewares.use(rewrite),
  };
}

// Public visitors must not download administrator code: fail the build when
// the public entry can reach an admin-only module.
function publicBundleGuard(): Plugin {
  const adminOnly = [`${src}/app/admin/`, `${src}/pages/admin/`].map((p) =>
    p.replaceAll("\\", "/"),
  );
  return {
    name: "redapp-public-bundle-guard",
    apply: "build",
    generateBundle(_options, bundle) {
      const chunks = new Map(
        Object.values(bundle)
          .filter((item) => item.type === "chunk")
          .map((chunk) => [chunk.fileName, chunk]),
      );
      const entry = [...chunks.values()].find((chunk) => chunk.isEntry && chunk.name === "index");
      if (!entry) return;
      const seen = new Set<string>();
      const queue = [entry.fileName];
      while (queue.length > 0) {
        const chunk = chunks.get(queue.pop() ?? "");
        if (!chunk || seen.has(chunk.fileName)) continue;
        seen.add(chunk.fileName);
        for (const id of chunk.moduleIds) {
          const path = id.replaceAll("\\", "/");
          if (adminOnly.some((prefix) => path.startsWith(prefix))) {
            this.error(`public entry reaches admin module ${path}`);
          }
        }
        queue.push(...chunk.imports, ...chunk.dynamicImports);
      }
    },
  };
}

// Every npm package that ships in the bundle must be recorded in
// third_party/README.md with its licence (AGENTS.md). Fail the build otherwise.
function thirdPartyGuard(): Plugin {
  const readme = fileURLToPath(new URL("../third_party/README.md", import.meta.url));
  return {
    name: "redapp-third-party-guard",
    apply: "build",
    generateBundle(_options, bundle) {
      const packages = new Set<string>();
      for (const item of Object.values(bundle)) {
        if (item.type !== "chunk") continue;
        for (const id of item.moduleIds) {
          const match = /node_modules\/((?:@[^/]+\/)?[^/]+)\//.exec(id.replaceAll("\\", "/"));
          if (match?.[1]) packages.add(match[1]);
        }
      }
      const recorded = readFileSync(readme, "utf8");
      const missing = [...packages].filter((name) => !recorded.includes(`\`${name}\``)).sort();
      if (missing.length > 0) {
        this.error(`bundled packages missing from third_party/README.md: ${missing.join(", ")}`);
      }
    },
  };
}

const backend = process.env.REDAPP_DEV_BACKEND;
const proxied = ["/api", "/admin/api", "/assets/icons", "/assets/presets"];

export default defineConfig({
  plugins: [vue(), tailwindcss(), adminDocument(), publicBundleGuard(), thirdPartyGuard()],
  resolve: { alias: { "@": src } },
  define: {
    // vue-i18n: composition API only, no devtools in production. Messages are
    // compiled by the CSP-safe JIT interpreter (no eval), as the SPA CSP requires.
    __VUE_I18N_LEGACY_API__: "false",
    __VUE_I18N_FULL_INSTALL__: "true",
    __INTLIFY_PROD_DEVTOOLS__: "false",
  },
  base: "/",
  server: backend
    ? {
        proxy: Object.fromEntries(
          proxied.map((prefix) => [prefix, { target: backend, changeOrigin: false }]),
        ),
      }
    : {},
  build: {
    outDir: "../internal/httpserver/web",
    emptyOutDir: true,
    // The server only serves flat /assets/*.{js,css,woff2,txt}; never inline
    // or emit other asset types.
    assetsInlineLimit: 0,
    rolldownOptions: {
      input: {
        index: fileURLToPath(new URL("./index.html", import.meta.url)),
        admin: fileURLToPath(new URL("./admin.html", import.meta.url)),
      },
    },
  },
  test: {
    environment: "happy-dom",
    environmentOptions: {
      happyDOM: {
        url: "http://localhost/",
        settings: { navigation: { disableChildFrameNavigation: true } },
      },
    },
    include: ["src/**/*.test.ts"],
    setupFiles: ["src/test/setup.ts"],
    restoreMocks: true,
  },
});
