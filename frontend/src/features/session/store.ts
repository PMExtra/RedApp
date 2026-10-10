import { computed, ref } from "vue";
import { defineStore } from "pinia";
import { api, isApiError, unwrap, type Schema } from "@/shared/api";

/**
 * - `unknown`: not checked yet.
 * - `signedIn`: a valid session; `csrfToken` is set.
 * - `signedOut`: no session (never signed in, signed out, or password changed).
 * - `expired`: the session ended while the admin was working; the page stays
 *   so drafts survive, and the shell asks to sign in again in a dialog.
 */
export type SessionStatus = "unknown" | "signedIn" | "signedOut" | "expired";
export type SessionNotice = "signedOut" | "passwordChanged" | "expired";

/** Administrator session per `/admin/api/session`. Client state only. */
export const useSessionStore = defineStore("session", () => {
  const status = ref<SessionStatus>("unknown");
  const csrfToken = ref<string | null>(null);
  const expiresAt = ref<string | null>(null);
  const notice = ref<SessionNotice | null>(null);
  /** A failed check that was not a 401 (network, storage). */
  const checkError = ref<unknown>(null);
  let expiryTimer: ReturnType<typeof setTimeout> | undefined;
  let pendingCheck: Promise<void> | null = null;

  function apply(session: Schema<"Session">) {
    csrfToken.value = session.csrf_token;
    expiresAt.value = session.expires_at;
    status.value = "signedIn";
    checkError.value = null;
    clearTimeout(expiryTimer);
    const remaining = Date.parse(session.expires_at) - Date.now();
    // setTimeout overflows above ~24.8 days; sessions last 8 hours.
    if (Number.isFinite(remaining) && remaining < 2 ** 31) {
      expiryTimer = setTimeout(expire, Math.max(0, remaining));
    }
  }

  function clear(next: SessionStatus, reason: SessionNotice | null) {
    clearTimeout(expiryTimer);
    csrfToken.value = null;
    expiresAt.value = null;
    status.value = next;
    notice.value = reason;
  }

  /** Called on 401 AUTH_REQUIRED and when `expires_at` passes. */
  function expire() {
    if (status.value === "signedIn") clear("expired", "expired");
  }

  /** GET the session; on start and when the tab becomes visible. */
  function check(): Promise<void> {
    pendingCheck ??= (async () => {
      try {
        apply(await unwrap(api.GET("/admin/api/session")));
      } catch (error) {
        if (isApiError(error, "AUTH_REQUIRED")) {
          if (status.value === "signedIn") clear("expired", "expired");
          else if (status.value !== "expired") clear("signedOut", notice.value);
        } else {
          checkError.value = error;
          if (status.value === "unknown") status.value = "signedOut";
        }
      } finally {
        pendingCheck = null;
      }
    })();
    return pendingCheck;
  }

  async function signIn(password: string) {
    const session = await unwrap(api.POST("/admin/api/session", { body: { password } }));
    apply(session);
    notice.value = null;
  }

  async function signOut() {
    try {
      await unwrap(api.DELETE("/admin/api/session"));
    } catch (error) {
      // Already gone on the server: the result is the same.
      if (!isApiError(error, "AUTH_REQUIRED")) throw error;
    }
    clear("signedOut", "signedOut");
  }

  /** On success every session is revoked, including this one. */
  async function changePassword(currentPassword: string, newPassword: string) {
    await unwrap(
      api.POST("/admin/api/password", {
        body: { current_password: currentPassword, new_password: newPassword },
      }),
    );
    clear("signedOut", "passwordChanged");
  }

  return {
    status,
    csrfToken,
    expiresAt,
    notice,
    checkError,
    signedIn: computed(() => status.value === "signedIn"),
    check,
    signIn,
    signOut,
    changePassword,
    expire,
  };
});
