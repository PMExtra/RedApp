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
