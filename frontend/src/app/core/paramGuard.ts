import type { Router } from "vue-router";
import { isSlug, isVendorId } from "@/shared/lib";

const UID = /^[0-9a-f]{32}$/;

/**
 * Shows the entry's not-found page for malformed `:vendor` / `:app` / `:upstream` segments.
 * A global guard, because `beforeEnter` does not run when only params change.
 */
export function installParamGuard(router: Router, notFound: string): void {
  router.beforeEach((to) => {
    const { vendor, app, upstream } = to.params;
    const valid =
      (vendor === undefined || isVendorId(String(vendor))) &&
      (app === undefined || isSlug(String(app))) &&
      (upstream === undefined || UID.test(String(upstream)));
    if (valid) return true;
    return { name: notFound, params: { pathMatch: to.path.slice(1).split("/") }, replace: true };
  });
}
