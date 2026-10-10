/*
 * Directory fixtures (vendors, applications, configuration overlays,
 * categories, notes, import previews), typed with the generated schema.
 */
import type { Schema } from "@/shared/api";
import { localized } from "./index";

type Overrides<T> = Partial<T>;

const CAPABILITIES: Record<Schema<"ProviderKey">, Schema<"Capabilities">> = {
  info: {
    details: true,
    instructions: true,
    hosted_files: false,
    files: false,
    versions: false,
    installers: false,
    time_cleanup: false,
  },
  hosted: {
    details: true,
    instructions: true,
    hosted_files: true,
    files: true,
    versions: false,
    installers: false,
    time_cleanup: false,
  },
  "http-cache": {
    details: true,
    instructions: true,
    hosted_files: false,
    files: true,
    versions: false,
    installers: false,
    time_cleanup: true,
  },
  codex: {
    details: true,
    instructions: true,
    hosted_files: false,
    files: true,
    versions: true,
    installers: true,
    time_cleanup: false,
  },
  "claude-code": {
    details: true,
    instructions: true,
    hosted_files: false,
    files: true,
    versions: true,
    installers: true,
    time_cleanup: false,
  },
};

export function providers(): Schema<"ProviderList"> {
  const item = (
    key: Schema<"ProviderKey">,
    name: string,
    base: string | null,
    ttl: number | null,
  ): Schema<"Provider"> => ({
    key,
    name: localized(name),
    description: localized(`${name} provider`),
    default_base_url: base,
    default_cache_ttl_seconds: ttl,
    capabilities: CAPABILITIES[key],
  });
  return {
    items: [
      item("info", "Information", null, null),
      item("hosted", "Hosted files", null, null),
      item("http-cache", "HTTP cache", null, 300),
      item("codex", "Codex", "https://releases.openai.com/codex", 60),
      item("claude-code", "Claude Code", "https://downloads.claude.ai/claude-code", 60),
    ],
  };
}

export function vendor(overrides: Overrides<Schema<"Vendor">> = {}): Schema<"Vendor"> {
  return {
    uid: "00112233445566778899aabbccddeeff",
    id: "example",
    name: localized("Example", "示例"),
    description: localized("Example vendor", "示例厂商"),
    icon: "",
    localized_icons: { en: "", "zh-CN": "" },
    enabled: true,
    has_template: false,
    revision: 3,
    deleted_at: null,
    ...overrides,
  };
}

export function app(overrides: Overrides<Schema<"App">> = {}): Schema<"App"> {
  const id = overrides.id ?? "tools";
  const vendorId = overrides.vendor_id ?? "example";
  return {
    uid: "ffeeddccbbaa99887766554433221100",
    id,
    key: `${vendorId}/${id}`,
    vendor_uid: "00112233445566778899aabbccddeeff",
    vendor_id: vendorId,
    name: localized("Tools", "工具"),
    description: localized("Internal tools", "内部工具"),
    icon: "",
    provider: "http-cache",
    base_urls: ["https://files.example.internal/tools"],
    source_strategy: "ordered",
    cache_ttl_seconds: 300,
    categories: [],
    tags: [],
    enabled: true,
    builtin_template: false,
    source_epoch: 1,
    revision: 9,
    deleted_at: null,
    ...overrides,
  };
}

export function appListItem(
  overrides: Overrides<Schema<"AppListItem">> = {},
): Schema<"AppListItem"> {
  return {
    ...app(overrides),
    latest_version: null,
    version_discovered_at: null,
    successful_downloads: 0,
    ...overrides,
  };
}

export function vendorListItem(
  overrides: Overrides<Schema<"VendorListItem">> = {},
): Schema<"VendorListItem"> {
  const apps = overrides.apps ?? [app()];
  return { ...vendor(overrides), apps, app_total: apps.length, ...overrides };
}

export function page<T>(
  items: T[],
  overrides: { page?: number; limit?: number; total?: number } = {},
) {
  const limit = overrides.limit ?? 20;
  const total = overrides.total ?? items.length;
  return {
    items,
    page: overrides.page ?? 1,
    limit,
    total,
    total_pages: Math.max(1, Math.ceil(total / limit)),
  };
}

const origin = (source: "inherited" | "custom", differs: boolean | null = false) => ({
  source,
  differs_from_template: differs,
});

/** A custom vendor (no template). */
export function vendorConfiguration(
  overrides: Overrides<Schema<"VendorConfiguration">> = {},
): Schema<"VendorConfiguration"> {
  const spec: Schema<"VendorSpec"> = {
    name: localized("Example", "示例"),
    description: localized("Example vendor", "示例厂商"),
    icon: "",
    localized_icons: { en: "", "zh-CN": "" },
    proxy: { mode: "inherit" },
  };
  return {
    revision: 3,
    template_ref: null,
    template_hash: null,
    template_missing: false,
    defaults: null,
    overrides: {},
    effective: spec,
    fields: Object.fromEntries(
      [
        "name.en",
        "name.zh-CN",
        "description.en",
        "description.zh-CN",
        "icon",
        "localized_icons.en",
        "localized_icons.zh-CN",
        "proxy",
      ].map((path) => [path, origin("custom", null)]),
    ),
    proxy_effective: { mode: "direct", source_scope: "global", source_id: "", dns: "local" },
    ...overrides,
  };
}

/** A built-in vendor linked to its template, with an overridden Chinese name. */
export function linkedVendorConfiguration(
  overrides: Overrides<Schema<"VendorConfiguration">> = {},
): Schema<"VendorConfiguration"> {
  const defaults: Schema<"VendorSpec"> = {
    name: localized("OpenAI"),
    description: localized("AI research company.", "人工智能公司。"),
    icon: "/assets/presets/openai/icon.svg",
    localized_icons: { en: "", "zh-CN": "" },
    proxy: { mode: "inherit" },
  };
  return vendorConfiguration({
    template_ref: "openai",
    template_hash: "a".repeat(64),
    defaults,
    effective: { ...defaults, name: { en: "OpenAI", "zh-CN": "开放人工智能" } },
    fields: {
      "name.en": origin("inherited"),
      "name.zh-CN": origin("custom", true),
      "description.en": origin("inherited"),
      "description.zh-CN": origin("inherited"),
      icon: origin("inherited"),
      "localized_icons.en": origin("inherited"),
      "localized_icons.zh-CN": origin("inherited"),
      proxy: origin("inherited"),
    },
    ...overrides,
  });
}

/** Configuration of the `http-cache` application from `app()`. */
export function appConfiguration(
  overrides: Overrides<Schema<"AppConfiguration">> = {},
): Schema<"AppConfiguration"> {
  const spec: Schema<"AppSpec"> = {
    name: localized("Tools", "工具"),
    description: localized("Internal tools", "内部工具"),
    icon: "",
    provider: "http-cache",
    proxy: { mode: "inherit" },
    categories: [],
    tags: [],
    instructions: localized("## Install", "## 安装"),
    base_urls: ["https://files.example.internal/tools"],
    source_strategy: "ordered",
    cache_ttl_seconds: 300,
    http_policy: { stale_fallback: false, rules: [], auto_cleanup: [] },
  };
  return {
    revision: 9,
    template_ref: null,
    template_hash: null,
    template_missing: false,
    defaults: null,
    overrides: {},
    effective: spec,
    fields: {},
    proxy_effective: { mode: "direct", source_scope: "global", source_id: "", dns: "local" },
    ...overrides,
  };
}

export function category(overrides: Overrides<Schema<"Category">> = {}): Schema<"Category"> {
  const name = overrides.name ?? localized("Developer tools", "开发工具");
  return {
    id: "developer-tools",
    name,
    builtin: false,
    applications: 2,
    revision: 1,
    template_ref: null,
    template_hash: null,
    template_missing: false,
    defaults: null,
    overrides: {},
    effective: { name },
    fields: { "name.en": origin("custom", null), "name.zh-CN": origin("custom", null) },
    ...overrides,
  };
}

export function notes(
  overrides: Overrides<Schema<"AdminNotesState">> = {},
): Schema<"AdminNotesState"> {
  return { text: "Rotate the upstream token in May.", revision: 2, ...overrides };
}

export function importItem(overrides: Overrides<Schema<"ImportItem">> = {}): Schema<"ImportItem"> {
  return {
    kind: "app",
    key: "example/tools",
    target: "example/tools",
    action: "update",
    uid: "ffeeddccbbaa99887766554433221100",
    revision: 9,
    notes_revision: 2,
    template_hash_mismatch: false,
    template_missing: false,
    detach_template: false,
    omitted_fields: [],
    differences: [],
    requirements: [],
    ...overrides,
  };
}

export function importPreview(
  overrides: Overrides<Schema<"ImportPreview">> = {},
): Schema<"ImportPreview"> {
  return {
    id: "5b1e0c9d8a7f6e5d4c3b2a1908f7e6d5",
    digest: "9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b",
    expires_at: new Date(Date.now() + 10 * 60 * 1000).toISOString(),
    ready: true,
    needs_instructions_trust: false,
    items: [importItem()],
    ...overrides,
  };
}
