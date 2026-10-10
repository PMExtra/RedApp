import type { Schema } from "@/shared/api";

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
