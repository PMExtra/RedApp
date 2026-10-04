import type { MatcherSpec } from "./cachePolicy";
import { t, type Message } from "./i18n";

export interface MaintenancePreview {
  id: string;
  kind: "cleanup" | "refresh";
  state: "building" | "ready" | "running" | "done" | "failed";
  match: MatcherSpec;
  basis?: "fetched_at" | "last_access";
  before?: string;
  created_at: string;
  expires_at: string;
  scanned_files: number;
  selected_files: number;
  selected_bytes: number;
  active_files: number;
  completed_files: number;
  failed_files: number;
  result?: unknown;
}
export interface RefreshSummary {
  selected_files: number;
  completed_files: number;
  refreshed: number;
  not_modified: number;
  stale_fallback: number;
  failed: number;
  skipped: number;
}
export interface PreviewItem {
  ordinal: number;
  generation_id: string;
  path: string;
  size_bytes: number;
  result_status: string;
  error_code?: string;
}
export interface RefreshItem {
  path: string;
  generation_id?: string;
  status: "refreshed" | "not_modified" | "stale_fallback" | "failed" | "skipped";
  reason?: string;
}
export function maintenanceStatus(value: string) {
  const names: Record<string, Message> = {
    building: "Building preview", ready: "Ready", running: "Running", done: "Finished", failed: "Failed",
    queued: "Queued", pending: "Pending", refreshed: "Refreshed", not_modified: "Not modified",
    stale_fallback: "Existing cached copy retained", skipped: "Skipped", retired: "Retired",
    skipped_accessed: "Accessed since preview", skipped_changed: "Generation changed",
  };
  return value ? names[value] ? t(names[value]) : value : t("Pending");
}
