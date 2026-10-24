/**
 * The public URL override: an http(s) origin without credentials, path,
 * query or fragment. One trailing "/" is allowed (the server removes it).
 */
export function isPublicOrigin(value: string): boolean {
  if (!/^https?:\/\/[^/?#@\s]+\/?$/i.test(value)) return false;
  try {
    const url = new URL(value);
    return url.hostname !== "" && url.pathname === "/";
  } catch {
    return false;
  }
}

/** Navigation URLs are also checked authoritatively by the server. */
export function validNavigationURL(raw: string): boolean {
  if (!raw || new TextEncoder().encode(raw).length > 2048) return false;
  try {
    const decoded = decodeURIComponent(raw);
    // eslint-disable-next-line no-control-regex
    if ([raw, decoded].some((value) => /[\s\x00-\x1f\x7f-\x9f\\]/u.test(value))) return false;
    if (raw.startsWith("/")) return !decoded.startsWith("//");
    const url = new URL(raw);
    return (
      /^https?:\/\/[^/?#@\s]+/.test(raw) &&
      ["http:", "https:"].includes(url.protocol) &&
      !!url.hostname &&
      !url.username &&
      !url.password
    );
  } catch {
    return false;
  }
}
