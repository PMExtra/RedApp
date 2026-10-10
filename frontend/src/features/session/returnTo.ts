export const DEFAULT_ADMIN_PATH = "/admin/overview";

/**
 * The page to open after sign-in. Only same-origin `/admin/...` paths are
 * accepted (never the login page itself); anything else falls back to the
 * overview, so `returnTo` cannot redirect elsewhere.
 */
export function safeReturnPath(value: unknown): string {
  if (typeof value !== "string" || !value.startsWith("/admin/") || /[\\\r\n]/.test(value)) {
    return DEFAULT_ADMIN_PATH;
  }
  try {
    const base = "https://redapp.invalid";
    const url = new URL(value, base);
    const path = decodeURIComponent(url.pathname);
    if (
      url.origin !== base ||
      !path.startsWith("/admin/") ||
      path.startsWith("/admin/login") ||
      path.startsWith("/admin/api/") ||
      // eslint-disable-next-line no-control-regex -- control characters are exactly what is rejected
      /[\\\x00-\x20]/.test(path)
    ) {
      return DEFAULT_ADMIN_PATH;
    }
    return url.pathname + url.search + url.hash;
  } catch {
    return DEFAULT_ADMIN_PATH;
  }
}
