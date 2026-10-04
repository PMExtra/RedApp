import { ref } from "vue";
import { api } from "./api";
import type { LocalizedText } from "./site";
import type { ProviderCapabilities } from "./bootstrap";

export type ProviderKey = "general-http" | "codex" | "claude-code";
export type SourceStrategy = "ordered" | "round_robin" | "random";
export interface Vendor {
  uid: string;
  id: string;
  name: LocalizedText;
  description: LocalizedText;
  icon: string;
  enabled: boolean;
  revision: number;
  deleted_at?: string | null;
}
export interface ManagedApplication extends Vendor {
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
  default_base_url: string;
  default_cache_ttl_seconds?: number;
  capabilities: ProviderCapabilities;
}
export const vendors = ref<Vendor[]>([]);
export const managedApps = ref<ManagedApplication[]>([]);
export const directoryLoading = ref(false);
export const directoryError = ref<unknown>();
let ticket = 0;
let controller: AbortController | undefined;
export function resetDirectory() {
  ticket++;
  controller?.abort();
  controller = undefined;
  vendors.value = [];
  managedApps.value = [];
  directoryLoading.value = false;
  directoryError.value = undefined;
}
export async function loadDirectory() {
  controller?.abort();
  const request = new AbortController(), attempt = ++ticket;
  controller = request;
  directoryLoading.value = true;
  directoryError.value = undefined;
  try {
    const [vendorList, appList] = await Promise.all([
      api<{ vendors: Vendor[] }>("vendors", undefined, request.signal),
      api<{ apps: ManagedApplication[] }>("apps", undefined, request.signal),
    ]);
    if (attempt !== ticket) return;
    if (!Array.isArray(vendorList.vendors) || !Array.isArray(appList.apps))
      throw Error("Invalid application directory");
    vendors.value = vendorList.vendors;
    managedApps.value = appList.apps;
  } catch (reason) {
    if (attempt === ticket) directoryError.value = reason;
  } finally {
    if (attempt === ticket) {
      controller = undefined;
      directoryLoading.value = false;
    }
  }
}
export function providerHasVersions(provider: string) {
  return provider === "codex" || provider === "claude-code";
}
export function providerHasTimeCleanup(provider: string) {
  return provider === "general-http";
}
export function applicationEnabled(app: ManagedApplication) {
  const vendor = vendors.value.find((item) => item.uid === app.vendor_uid);
  return app.enabled && !app.deleted_at && !!vendor?.enabled && !vendor.deleted_at;
}
