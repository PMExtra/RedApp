/** Vendor IDs that collide with top-level routes (`internal/identity`). */
export const RESERVED_VENDOR_IDS: ReadonlySet<string> = new Set([
  "admin",
  "api",
  "assets",
  "health",
  "all",
]);

const SLUG = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

export function isSlug(value: string): boolean {
  return value.length <= 63 && SLUG.test(value);
}

export function isVendorId(value: string): boolean {
  return isSlug(value) && !RESERVED_VENDOR_IDS.has(value);
}

/** Public distribution URL of a file below an application (`/vendor/app/a/b c.txt`). */
export function distributionPath(vendor: string, app: string, filePath: string): string {
  const encoded = filePath
    .split("/")
    .map((segment) => encodeURIComponent(segment))
    .join("/");
  return `/${vendor}/${app}/${encoded}`;
}

/** Absolute URL from the effective public URL of `getBootstrap` (`public_url`). */
export function publicUrl(base: string, path: string): string {
  return base.replace(/\/+$/, "") + path;
}
