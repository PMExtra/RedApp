import type { RouteLocationRaw } from "vue-router";
import type { App, ProviderKey, Vendor } from "./queries";

/** Tabs of the application pages, in display order. */
export type AppTab =
  "settings" | "versions" | "cache" | "metrics" | "files" | "events" | "admin-notes";

const TAB_ROUTES: Record<AppTab, string> = {
  settings: "admin-app-settings",
  versions: "admin-app-versions",
  cache: "admin-app-cache",
  metrics: "admin-app-metrics",
  files: "admin-app-files",
  events: "admin-app-events",
  "admin-notes": "admin-app-notes",
};

/** Providers with a base URL that defaults from the provider (release protocols). */
export function isReleaseProvider(provider: ProviderKey): boolean {
  return provider === "codex" || provider === "claude-code";
}

/** Providers whose releases the versions tab lists. */
export function hasVersions(provider: ProviderKey): boolean {
  return isReleaseProvider(provider) || provider === "json-release";
}

/** Providers with cache maintenance (cached paths, or version cleanup). */
export function hasCache(provider: ProviderKey): boolean {
  return provider === "http-cache" || hasVersions(provider);
}

export const hasMetrics = hasCache;

export function hasHostedFiles(provider: ProviderKey): boolean {
  return provider === "hosted";
}

/**
 * Tabs an application offers: files for `hosted`; cache and metrics for
 * `http-cache`, `codex`, `claude-code` and `json-release`; versions for the
 * release providers (not for deleted applications, whose release data is no
 * longer browsable); events for every application.
 */
export function appTabs(app: Pick<App, "provider" | "deleted_at">): AppTab[] {
  const tabs: AppTab[] = ["settings"];
  if (hasVersions(app.provider) && !app.deleted_at) tabs.push("versions");
  if (hasCache(app.provider)) tabs.push("cache");
  if (hasMetrics(app.provider)) tabs.push("metrics");
  if (hasHostedFiles(app.provider)) tabs.push("files");
  tabs.push("events", "admin-notes");
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

/** Pages below a tab; the tab stays selected on them. */
const SUBPAGE_TABS: Record<string, AppTab> = { "admin-app-version": "versions" };

export function appTabFromRoute(name: unknown): AppTab | undefined {
  if (typeof name === "string" && name in SUBPAGE_TABS) return SUBPAGE_TABS[name];
  return (Object.keys(TAB_ROUTES) as AppTab[]).find((tab) => TAB_ROUTES[tab] === name);
}

/** The stored files of one version of a release application. */
export function versionRoute(vendor: string, app: string, version: string): RouteLocationRaw {
  return { name: "admin-app-version", params: { vendor, app, version } };
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
