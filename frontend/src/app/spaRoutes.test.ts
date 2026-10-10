import { describe, expect, it } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { spaRoutes } from "@/shared/api";
import { adminRoutes } from "./admin/routes";
import { publicRoutes } from "./public/routes";

/** `/admin/vendors/:vendor` → `/admin/vendors/{vendor}` (spec notation). */
function specPath(path: string): string {
  return path.replace(/:(\w+)/g, "{$1}").replace(/(.)\/$/, "$1") || "/";
}

function namedPaths(routes: typeof adminRoutes): string[] {
  const router = createRouter({ history: createMemoryHistory(), routes });
  return router
    .getRoutes()
    .filter((route) => route.name !== undefined && !String(route.name).endsWith("not-found"))
    .map((route) => specPath(route.path));
}

describe("SPA routes", () => {
  // The server serves the SPA document only for x-spa-routes; a client route
  // missing there works when navigated to but 404s on reload or deep link.
  it("match the spec's x-spa-routes exactly", () => {
    const client = [...namedPaths(publicRoutes), ...namedPaths(adminRoutes)].sort();
    expect(client).toEqual([...spaRoutes].sort());
  });

  it("keep admin pages in the admin entry and public pages in the public entry", () => {
    expect(namedPaths(publicRoutes).every((path) => !path.startsWith("/admin"))).toBe(true);
    expect(namedPaths(adminRoutes).every((path) => path.startsWith("/admin/"))).toBe(true);
  });
});
