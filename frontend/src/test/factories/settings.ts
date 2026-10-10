import type { Schema } from "@/shared/api";
import { localized } from "./index";

type Overrides<T> = Partial<T>;

export function publicUrlState(
  overrides: Overrides<Schema<"PublicUrlState">> = {},
): Schema<"PublicUrlState"> {
  return {
    override_url: null,
    environment_url: null,
    effective_url: "http://localhost",
    source: "request",
    revision: 2,
    ...overrides,
  };
}

export function homepageState(
  overrides: Overrides<Schema<"HomepageSettingsState">> = {},
): Schema<"HomepageSettingsState"> {
  return { pinned_app_keys: ["openai/codex", "anthropic/claude-code"], revision: 5, ...overrides };
}

export function globalProxyState(
  overrides: Overrides<Schema<"GlobalProxyState">> = {},
): Schema<"GlobalProxyState"> {
  return {
    mode: "url",
    url: "socks5://user:****@proxy.example.internal:1080",
    dns: "proxy",
    revision: 4,
    ...overrides,
  };
}

export function appListItem(
  overrides: Overrides<Schema<"AppListItem">> = {},
): Schema<"AppListItem"> {
  return {
    uid: "0".repeat(32),
    id: "gemini-cli",
    key: "google/gemini-cli",
    vendor_uid: "1".repeat(32),
    vendor_id: "google",
    name: localized("Gemini CLI"),
    description: localized("Google's terminal agent.", "Google 的终端助手。"),
    icon: "",
    provider: "http-cache",
    categories: [],
    tags: [],
    enabled: true,
    builtin_template: false,
    source_epoch: 1,
    revision: 1,
    deleted_at: null,
    latest_version: null,
    version_discovered_at: null,
    successful_downloads: 0,
    ...overrides,
  };
}

export function appPage(items: Schema<"AppListItem">[]): Schema<"AppPage"> {
  return { items, page: 1, limit: 8, total: items.length, total_pages: 1 };
}

export function operationalEvent(
  id: number,
  overrides: Overrides<Schema<"Event">> = {},
): Schema<"Event"> {
  return {
    id,
    time: new Date(Date.UTC(2026, 9, 10, 12, 0, 0) - id * 60_000).toISOString(),
    category: "http",
    code: "UPSTREAM_STATUS",
    message: `Upstream returned 502 (event ${String(id)})`,
    status_code: 502,
    app_key: "openai/codex",
    version: "0.46.0",
    resource_key: "codex-x86_64-unknown-linux-musl.tar.gz",
    generation_id: null,
    ...overrides,
  };
}
