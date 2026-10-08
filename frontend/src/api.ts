import { language, t } from "./i18n";
import { appAPI } from "./bootstrap";
export interface Resource {
  ID: string;
  State: string;
  Bytes: number;
  Readers: number;
  Retired: boolean;
  AverageBPS: number;
  RecentBPS: number;
  Resumes: number;
  VerificationNS: number;
  Started: string;
  Finished: string;
  Error: string;
  Resource: {
    Application: string;
    Version: string;
    Key: string;
    Labels: { name?: string };
  };
}
export interface Status {
  metrics?: Metric[];
  name: string;
  public_base_url: string;
  sampled_at: string;
  os: string;
  arch: string;
  go: string;
  goroutines: number;
  memory_bytes: number;
  client_runtime_update_policy: string;
  disk: {
    cache_bytes: number;
    temporary_bytes: number;
    pending_bytes: number;
    used_bytes: number;
    free_bytes: number;
  };
  rates: {
    upstream_bytes_per_second: number;
    downstream_bytes_per_second: number;
  };
  counters: Record<string, number>;
}
export interface VersionSummary {
  version: string;
  first_seen: string;
  requests: number;
  bytes: number;
}
export interface DistributionEvent {
  time: string;
  resource: string;
  app_id?: string;
  version?: string;
  resource_key?: string;
  generation_id?: string;
  code?: string;
  category: string;
  message: string;
  status_code?: number;
}
export interface Page<T> {
  items: T[];
  next_cursor: string | null;
}

export interface CleanupPreview {
  job: {
    ID: string;
    Selected: Array<{ Resource: string; Generation: string }>;
  };
  logical_bytes: number;
  reclaimable_blob_bytes: number;
  active: number;
  unknown_versions: string[];
}
export interface ProxySettings {
 mode?: "direct"|"url";
 url?: string;
  server: string;
  dns: string;
}
export interface APIProblem {
  code?: string;
  message?: string;
  request_id?: string;
  retryable?: boolean;
}
export class ApiError extends Error {
  readonly code?: string;
  readonly request_id?: string;
  readonly retryable?: boolean;
  constructor(
    problem: string | APIProblem,
    public status: number,
  ) {
    super(
      typeof problem === "string"
        ? problem
        : typeof problem.message === "string"
          ? problem.message
          : "Request failed",
    );
    this.name = "ApiError";
    if (typeof problem === "object") {
      this.code = typeof problem.code === "string" ? problem.code : undefined;
      this.request_id =
        typeof problem.request_id === "string" ? problem.request_id : undefined;
      this.retryable =
        typeof problem.retryable === "boolean" ? problem.retryable : undefined;
    }
  }
}
let csrf = "";
let sessionGeneration = 0;
let unauthorized: (() => void) | undefined;
export function setUnauthorizedHandler(handler: () => void) {
  unauthorized = handler;
}
export function isCancellation(error: unknown): boolean {
  return error instanceof Error && error.name === "AbortError";
}
// Revalidation adopts an unchanged session without revoking its active requests.
export function recheckCSRF(value: string) {
  if (value !== csrf) setCSRF(value);
}
export function setCSRF(value: string) {
  // Every session transition revokes old requests, even if a token is reused.
  sessionGeneration++;
  csrf = value;
}
export async function fetchResponse(
  input: RequestInfo | URL,
  init?: RequestInit,
): Promise<Response> {
  try {
    return await fetch(input, init);
  } catch (reason) {
    if (init?.signal?.aborted || isCancellation(reason))
      throw new DOMException("Cancelled", "AbortError");
    throw new ApiError(
      { code: "NETWORK_ERROR", message: "Network request failed" },
      0,
    );
  }
}
export async function api<T>(
  path: string,
  body?: unknown,
  signal?: AbortSignal,
  extraHeaders: Record<string, string> = {},
  method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE",
): Promise<T> {
  const generation = sessionGeneration;
  const response = await fetchResponse("/admin/api/" + path, {
    method: method || (body === undefined ? "GET" : "POST"),
    credentials: "same-origin",
    headers: {
      ...(body instanceof FormData
        ? {}
        : { "Content-Type": "application/json" }),
      "X-CSRF-Token": csrf,
      ...extraHeaders,
    },
    body:
      body === undefined
        ? undefined
        : body instanceof FormData
          ? body
          : JSON.stringify(body),
    signal,
  }).catch((reason) => {
    if (generation !== sessionGeneration)
      throw new DOMException("Session changed", "AbortError");
    throw reason;
  });
  let data: any,
    invalidJSON = false;
  try {
    data = await response.json();
  } catch {
    invalidJSON = true;
  }
  // Fetch may have resolved before cancellation, while its JSON body was pending.
  // Check ownership before returning sensitive data or invoking the global 401 handler.
  if (signal?.aborted || generation !== sessionGeneration)
    throw new DOMException(
      "Request no longer belongs to the active session",
      "AbortError",
    );
  if (!response.ok) {
    if (response.status === 401) unauthorized?.();
    const problem = data?.error;
    throw new ApiError(
      typeof problem === "string" || (problem && typeof problem === "object")
        ? problem
        : "Request failed",
      response.status,
    );
  }
  if (invalidJSON)
    throw new ApiError(
      { code: "INVALID_RESPONSE", message: "Invalid server response" },
      response.status,
    );
  return data as T;
}
export function bytes(value: number | null | undefined): string {
  if (
    value === null ||
    value === undefined ||
    !Number.isFinite(value) ||
    value < 0
  )
    return "—";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  const index =
    value === 0
      ? 0
      : Math.min(
          Math.floor(Math.log(value) / Math.log(1024)),
          units.length - 1,
        );
  return `${(value / 1024 ** Math.max(0, index)).toFixed(2)} ${units[Math.max(0, index)]}`;
}

export interface Metric {
  key: string;
  label: string;
  kind: "gauge" | "counter" | "rate";
  unit: "bytes" | "bytes_per_second" | "count" | "seconds";
  group: string;
  value: number | null;
  observed_seconds: number;
}
export interface HistoryPoint {
  time: number;
  value: number | null;
  min: number | null;
  max: number | null;
  avg: number | null;
  last: number | null;
  count: number;
  delta: number | null;
  delta_count: number;
  observed_seconds: number;
  partial: boolean;
  incomplete: boolean;
}
export interface HistorySeries
  extends Omit<Metric, "value" | "observed_seconds"> {
  range: string;
  resolution_seconds: number;
  from: number;
  to: number;
  points: HistoryPoint[];
  retired?: boolean;
}
export function formatDuration(value: number | null | undefined): string {
  if (value == null || !Number.isFinite(value) || value < 0) return "—";
  const units = [
    { seconds: 1, label: "s" },
    { seconds: 60, label: "min" },
    { seconds: 3600, label: "h" },
    { seconds: 86400, label: "d" },
  ] as const;
  let index = 0;
  while (
    index < units.length - 1 &&
    Number((value / units[index]!.seconds).toFixed(2)) *
      units[index]!.seconds >=
      units[index + 1]!.seconds
  )
    index++;
  const unit = units[index]!;
  return `${(value / unit.seconds).toLocaleString(language.value, { maximumFractionDigits: 2 })} ${t(unit.label)}`;
}
export function formatMetric(
  value: number | null | undefined,
  unit: Metric["unit"],
): string {
  if (
    value === null ||
    value === undefined ||
    !Number.isFinite(value) ||
    value < 0
  )
    return "—";
  if (unit === "bytes") return bytes(value);
  if (unit === "bytes_per_second") return bytes(value) + "/s";
  if (unit === "seconds") return formatDuration(value);
  return value.toLocaleString(language.value, { maximumFractionDigits: 2 });
}
export function getHistory(
  key: string,
  range: string,
  signal?: AbortSignal,
  application?: string,
) {
  const path = application ? `${appAPI(application)}/history` : "history";
  return api<HistorySeries>(
    `${path}?metric=${encodeURIComponent(key)}&range=${encodeURIComponent(range)}${application ? "" : "&scope=global"}`,
    undefined,
    signal,
  );
}

export function putSetting<T>(
  path: string,
  body: unknown,
  revision: number,
  signal?: AbortSignal,
) {
  return api<T>(path, body, signal, { "If-Match": `"${revision}"` }, "PUT");
}

export async function configurationDownload(body: unknown, signal?: AbortSignal): Promise<Blob> {
  const generation=sessionGeneration;
  const response=await fetchResponse('/admin/api/configuration/export',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:JSON.stringify(body),signal});
  if(!response.ok){if(response.status===401)unauthorized?.();const data=await response.json().catch(()=>({}));throw new ApiError(data.error||'Export failed',response.status)}
  const blob=await response.blob();
  if(signal?.aborted||generation!==sessionGeneration)throw new DOMException('Session changed','AbortError');
  return blob;
}
