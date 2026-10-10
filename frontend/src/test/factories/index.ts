/*
 * Test fixtures built from the spec's examples and typed with the generated
 * schema. Each function returns a fresh object; pass overrides for the
 * fields a test cares about. Feature packages add their own sibling files
 * (one per domain, e.g. `directory.ts`) and import them directly, so
 * packages never edit the same factory file.
 */
import type { Schema } from "@/shared/api";

type Overrides<T> = Partial<T>;

export function localized(en: string, zh = en): Schema<"LocalizedText"> {
  return { en, "zh-CN": zh };
}

export function siteSettings(
  overrides: Overrides<Schema<"SiteSettings">> = {},
): Schema<"SiteSettings"> {
  return {
    title: localized("RedApp Mirror", "RedApp 镜像"),
    subtitle: localized("Internal application mirror", "内部应用镜像"),
    disclaimer: localized("For internal use only.", "仅供内部使用。"),
    ...overrides,
  };
}

export function bootstrap(overrides: Overrides<Schema<"Bootstrap">> = {}): Schema<"Bootstrap"> {
  return {
    version: "0.9.0",
    os: "linux",
    arch: "amd64",
    site: siteSettings(),
    public_url: "https://mirror.example.internal",
    revision: "b1",
    ...overrides,
  };
}

export function session(overrides: Overrides<Schema<"Session">> = {}): Schema<"Session"> {
  return {
    csrf_token: "c".repeat(64),
    expires_at: new Date(Date.now() + 8 * 3600 * 1000).toISOString(),
    ...overrides,
  };
}

export function publicVendor(
  overrides: Overrides<Schema<"PublicVendor">> = {},
): Schema<"PublicVendor"> {
  return {
    id: "openai",
    name: localized("OpenAI"),
    description: localized("AI research and deployment company.", "人工智能研究与部署公司。"),
    icon: "/assets/presets/openai/icon.svg",
    localized_icons: { en: "", "zh-CN": "" },
    ...overrides,
  };
}

export function publicApp(overrides: Overrides<Schema<"PublicApp">> = {}): Schema<"PublicApp"> {
  return {
    key: "openai/codex",
    id: "codex",
    vendor: publicVendor(),
    name: localized("Codex CLI"),
    description: localized("OpenAI’s coding agent for your terminal.", "OpenAI 的终端编程助手。"),
    icon: "/assets/presets/openai/codex/icon.svg",
    provider: "codex",
    capabilities: {
      details: true,
      instructions: true,
      hosted_files: false,
      files: true,
      versions: true,
      installers: true,
      time_cleanup: false,
    },
    categories: [],
    latest_known_version: { version: "0.46.0", first_seen: "2026-10-01T08:00:00Z" },
    instructions_available: { en: true, "zh-CN": true },
    revision: "r1",
    ...overrides,
  };
}

export function home(overrides: Overrides<Schema<"Home">> = {}): Schema<"Home"> {
  return {
    pinned: [publicApp()],
    ranking: [{ app: publicApp(), download_clients: 12 }],
    window_hours: 168,
    ...overrides,
  };
}

export function searchHit(overrides: Overrides<Schema<"SearchHit">> = {}): Schema<"SearchHit"> {
  return {
    kind: "app",
    key: "openai/codex",
    name: localized("Codex CLI"),
    icon: "/assets/presets/openai/codex/icon.svg",
    localized_icons: null,
    ...overrides,
  };
}

export function siteSettingsState(
  overrides: Overrides<Schema<"SiteSettingsState">> = {},
): Schema<"SiteSettingsState"> {
  return { ...siteSettings(), revision: 3, ...overrides };
}
