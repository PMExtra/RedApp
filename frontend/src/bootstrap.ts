import { ref } from "vue";
import {
  applySite,
  siteRevision,
  type LocalizedText,
  type SiteSettings,
} from "./site";
export interface Application {
  categories?: { id: string; name: LocalizedText }[];
  id: string;
  name: LocalizedText;
  publisher: string;
  latest_known_version?: { version: string; first_seen: string | null } | null;
  vendor?: { id: string; name: LocalizedText; description?: LocalizedText; icon?: string; localized_icons?: LocalizedText };
  summary: LocalizedText;
  instructions?: LocalizedText;
  icon: string;
  channels: string[];
  installers: { file: string; shell: "sh" | "bash" | "powershell" }[];
  update_policy: LocalizedText;
  command?: string;
  provider?: string;
  capabilities?: ProviderCapabilities;
}
export interface ProviderCapabilities {
  details?: boolean;
  instructions?: boolean;
  files?: boolean;
  hosted_files?: boolean;
  versions: boolean;
  installers: boolean;
  time_cleanup: boolean;
}
export function applicationCapabilities(
  app: Application,
): ProviderCapabilities {
  if (app.capabilities) return app.capabilities;
  // Capabilities are supplied by the compiled provider registry. Missing or
  // unrecognized metadata must never grant release or file behavior.
  return {
    details: true,
    instructions: true,
    files: false,
    hosted_files: false,
    versions: false,
    installers: false,
    time_cleanup: false,
  };
}
export interface Bootstrap {
  version: string;
  os: string;
  arch: string;
  site: SiteSettings;
  apps: Application[];
  public_origin: string;
  revision: string;
}
export const bootstrap = ref<Bootstrap>();
export const bootstrapLoading = ref(false);
export const bootstrapError = ref<unknown>();
let controller: AbortController | undefined,
  ticket = 0;
export function invalidateBootstrap() {
  ticket++;
  controller?.abort();
  controller = undefined;
  bootstrapLoading.value = false;
}
export async function loadBootstrap() {
  if (controller) return;
  const attempt = ++ticket,
    currentSite = siteRevision.value;
  const request = new AbortController();
  controller = request;
  bootstrapLoading.value = true;
  bootstrapError.value = undefined;
  let timeout: ReturnType<typeof setTimeout> | undefined;
  try {
    const deadline = new Promise<never>((_, reject) => {
      timeout = setTimeout(() => {
        request.abort();
        reject(Error("bootstrap timeout"));
      }, 10000);
    });
    const value = await Promise.race([
      fetch("/api/bootstrap", {
        credentials: "omit",
        cache: "no-store",
        signal: request.signal,
      }).then(async (response) => {
        if (!response.ok) throw Error("bootstrap unavailable");
        return (await response.json()) as Bootstrap;
      }),
      deadline,
    ]);
    if (attempt === ticket) {
      bootstrap.value = value;
      applySite(value.site, currentSite);
    }
  } catch (reason) {
    if (attempt === ticket) bootstrapError.value = reason;
  } finally {
    clearTimeout(timeout);
    if (attempt === ticket) {
      controller = undefined;
      bootstrapLoading.value = false;
    }
  }
}
export function applyPublicOrigin(value: string) {
  invalidateBootstrap();
  if (bootstrap.value)
    bootstrap.value = { ...bootstrap.value, public_origin: value };
}
export function validApplicationID(id: string): boolean {
  const parts = id.split("/");
  return (
    parts.length === 2 &&
    !["admin", "api", "assets", "health", "all"].includes(parts[0]!) &&
    parts.every((p) => p.length <= 63 && /^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(p))
  );
}
export function appAPI(id: string): string {
  if (!validApplicationID(id)) throw Error("Invalid application ID");
  return `apps/${id}`;
}
