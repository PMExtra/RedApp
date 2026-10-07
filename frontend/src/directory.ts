import { ref } from "vue";
import { api, isCancellation } from "./api";
import type { LocalizedText } from "./site";
import type { ProviderCapabilities } from "./bootstrap";

export const applicationNavigation = { en: "Applications", "zh-CN": "应用管理" };

export type ProviderKey =
  | "info"
  | "hosted"
  | "http-cache"
  | "codex"
  | "claude-code";
export type SourceStrategy = "" | "ordered" | "round_robin" | "random";
export interface DirectoryEntity {
  has_template?: boolean;
  uid: string;
  id: string;
  name: LocalizedText;
  description: LocalizedText;
  icon: string;
  enabled: boolean;
  revision: number;
  deleted_at?: string | null;
}
export interface Vendor extends DirectoryEntity { localized_icons?: LocalizedText; }
export interface ManagedApplication extends DirectoryEntity {
  builtin_template?: boolean;
  key: string;
  vendor_uid: string;
  vendor_id: string;
  provider: ProviderKey;
  base_url: string;
  base_urls?: string[];
  source_strategy?: SourceStrategy;
  cache_ttl_seconds: number;
  source_epoch: number;
}
export interface ProviderDefinition {
  key: ProviderKey;
  name: LocalizedText;
  description?: LocalizedText;
  default_base_url: string;
  default_cache_ttl_seconds?: number;
  capabilities: ProviderCapabilities;
}
// Detail state contains exactly one application and its parent, never all pages.
export const applicationRecord = ref<ManagedApplication>();
export const vendorRecord = ref<Vendor>();
export const directoryLoading = ref(false);
export const directoryError = ref<unknown>();
let ticket = 0,
  controller: AbortController | undefined;
let selectedKey = "";
export function resetDirectory() {
  ticket++;
  controller?.abort();
  controller = undefined;
  selectedKey = "";
  applicationRecord.value = undefined;
  vendorRecord.value = undefined;
  directoryLoading.value = false;
  directoryError.value = undefined;
}
export async function loadApplication(key: string, preserve = false) {
  if (!preserve || selectedKey !== key) resetDirectory();
  else {
    ticket++;
    controller?.abort();
    controller = undefined;
  }
  selectedKey = key;
  directoryError.value = undefined;
  const request = new AbortController(),
    attempt = ++ticket;
  controller = request;
  directoryLoading.value = true;
  try {
    const [a, v] = await Promise.all([
      api<{ app: ManagedApplication }>(
        `apps/${key}`,
        undefined,
        request.signal,
      ),
      api<{ vendor: Vendor }>(
        `vendors/${key.split("/")[0]}`,
        undefined,
        request.signal,
      ),
    ]);
    if (attempt !== ticket) return;
    if (a.app?.key !== key || v.vendor?.uid !== a.app.vendor_uid)
      throw Error("Invalid application details");
    applicationRecord.value = a.app;
    vendorRecord.value = v.vendor;
  } catch (reason) {
    if (attempt === ticket) directoryError.value = reason;
  } finally {
    if (attempt === ticket) {
      controller = undefined;
      directoryLoading.value = false;
    }
  }
}
export function refreshApplication() {
  return selectedKey ? loadApplication(selectedKey, true) : Promise.resolve();
}
export function applicationPath(app: Pick<ManagedApplication, "vendor_id" | "id"> & { provider: string }, tab?: string) {
  return `/admin/vendors/${app.vendor_id}/apps/${app.id}/${tab || (providerHasVersions(app.provider) ? "versions" : app.provider === "info" ? "settings" : app.provider === "hosted" ? "files" : "cache")}`;
}
export function providerHasVersions(provider: string) {
  return provider === "codex" || provider === "claude-code";
}
export function applicationEnabled(app: ManagedApplication) {
  const vendor =
    vendorRecord.value?.uid === app.vendor_uid ? vendorRecord.value : undefined;
  return (
    app.enabled && !app.deleted_at && !!vendor?.enabled && !vendor.deleted_at
  );
}

// Compiled brand icons remain visible to administrators when an app is disabled.
export function directoryIcon(path: string): string {
  return path && !path.startsWith("/assets/icons/")
    ? `/admin/api/assets/builtin-icon?path=${encodeURIComponent(path)}`
    : path;
}

// Shared immediate availability update: never include unsaved profile fields.
export function patchEntityEnabled(kind: "vendor" | "app", key: string, revision: number, enabled: boolean, signal: AbortSignal) {
  return api<{ vendor?: Vendor; app?: ManagedApplication }>(`${kind === "vendor" ? "vendors" : "apps"}/${key}`, { revision, enabled }, signal, {}, "PATCH");
}
