import type { Router } from "vue-router";
import { safeReturnPath, useSessionStore } from "@/features/session";

/**
 * - The first navigation reads the session.
 * - Protected pages need a session; otherwise go to sign-in with `returnTo`.
 *   An expired session stays on the page (the shell asks to sign in again).
 * - The sign-in page forwards signed-in admins to `returnTo`.
 */
export function installSessionGuard(router: Router): void {
  router.beforeEach(async (to) => {
    const session = useSessionStore();
    if (session.status === "unknown") await session.check();
    if (to.meta.public) {
      return session.signedIn ? safeReturnPath(to.query.returnTo) : true;
    }
    if (session.status === "signedIn" || session.status === "expired") return true;
    return { name: "admin-login", query: { returnTo: to.fullPath }, replace: true };
  });
}
