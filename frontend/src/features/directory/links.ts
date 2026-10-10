import type { RouteLocationRaw } from "vue-router";
import type { App, ProviderKey, Vendor } from "./queries";

/** Tabs of the application pages, in display order. */
export type AppTab = "settings" | "versions" | "cache" | "files" | "admin-notes";

const TAB_ROUTES: Record<AppTab, string> = {
  settings: "admin-app-settings",
  versions: "admin-app-versions",
  cache: "admin-app-cache",
  files: "admin-app-files",
  "admin-notes": "admin-app-notes",
};

export function hasVersions(provider: ProviderKey): boolean {
  return provider === "codex" || provider === "claude-code";
}

/** Providers with a base URL that defaults from the provider (release protocols). */
export const isReleaseProvider = hasVersions;

export function hasCache(provider: ProviderKey): boolean {
  return provider === "http-cache" || hasVersions(provider);
}

export function hasHostedFiles(provider: ProviderKey): boolean {
  return provider === "hosted";
}

/**
 * Tabs an application offers: files for `hosted`; cache for `http-cache`,
 * `codex` and `claude-code`; versions for `codex` and `claude-code` (not for
 * deleted applications, whose release data is no longer browsable).
 */
export function appTabs(app: Pick<App, "provider" | "deleted_at">): AppTab[] {
  const tabs: AppTab[] = ["settings"];
  if (hasVersions(app.provider) && !app.deleted_at) tabs.push("versions");
  if (hasCache(app.provider)) tabs.push("cache");
  if (hasHostedFiles(app.provider)) tabs.push("files");
  tabs.push("admin-notes");
  return tabs;
}

/** The tab an application link opens: its main runtime page, else settings. */
export function defaultAppTab(app: Pick<App, "provider" | "deleted_at">): AppTab {
  if (app.deleted_at) return "settings";
  if (hasVersions(app.provider)) return "versions";
  if (app.provider === "http-cache") return "cache";
  if (app.provider === "hosted") return "files";
  return "settings";
}

export function appTabFromRoute(name: unknown): AppTab | undefined {
  return (Object.keys(TAB_ROUTES) as AppTab[]).find((tab) => TAB_ROUTES[tab] === name);
}

export function appRoute(
  app: Pick<App, "vendor_id" | "id" | "provider" | "deleted_at">,
  tab: AppTab = defaultAppTab(app),
): RouteLocationRaw {
  return { name: TAB_ROUTES[tab], params: { vendor: app.vendor_id, app: app.id } };
}

export function vendorRoute(
  vendor: string,
  tab: "settings" | "apps" | "admin-notes" = "settings",
  query?: Record<string, string>,
): RouteLocationRaw {
  const name = {
    settings: "admin-vendor-settings",
    apps: "admin-vendor-apps",
    "admin-notes": "admin-vendor-notes",
  }[tab];
  return { name, params: { vendor }, query };
}

/** Visible on the public site: enabled, not deleted, vendor enabled. */
export function isPublished(
  app: Pick<App, "enabled" | "deleted_at">,
  vendor: Pick<Vendor, "enabled" | "deleted_at"> | undefined,
): boolean {
  return app.enabled && !app.deleted_at && !!vendor?.enabled && !vendor.deleted_at;
}
