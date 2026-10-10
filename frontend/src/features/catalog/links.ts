import type { Schema } from "@/shared/api";
import type { Locale } from "@/shared/i18n";

type Capabilities = Schema<"Capabilities">;

/**
 * The admin tab that best matches the application: versions for release
 * apps, files for hosted apps, the cache for other file-serving apps and
 * settings otherwise. Only tabs listed in `x-spa-routes` are used.
 */
export function adminAppPath(vendor: string, app: string, capabilities?: Capabilities): string {
  let tab = "settings";
  if (capabilities?.versions) tab = "versions";
  else if (capabilities?.hosted_files) tab = "files";
  else if (capabilities?.files) tab = "cache";
  return `/admin/vendors/${vendor}/apps/${app}/${tab}`;
}

/** The vendor logo in `locale`; an empty localized logo falls back to `icon`. */
export function vendorLogo(
  vendor: Pick<Schema<"PublicVendor">, "icon" | "localized_icons">,
  locale: Locale,
): string {
  return vendor.localized_icons[locale] || vendor.icon;
}

/** Parses a `?page=` value; anything but a positive integer is page 1. */
export function parsePage(value: unknown): number {
  if (typeof value !== "string" || !/^[1-9]\d{0,8}$/.test(value)) return 1;
  return Number(value);
}
