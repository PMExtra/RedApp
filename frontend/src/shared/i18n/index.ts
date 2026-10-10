import { createI18n, type I18n } from "vue-i18n";
import { mergeLocaleModules, type LocaleGlob, type LocaleMessages } from "./messages";

export const SUPPORTED_LOCALES = ["en", "zh-CN"] as const;
export type Locale = (typeof SUPPORTED_LOCALES)[number];
export const DEFAULT_LOCALE: Locale = "en";
const STORAGE_KEY = "redapp-language";

// eslint-disable-next-line @typescript-eslint/no-empty-object-type -- vue-i18n defaults
export type AppI18n = I18n<Record<Locale, LocaleMessages>, {}, {}, Locale, false>;

let active: AppI18n | undefined;

export function isLocale(value: unknown): value is Locale {
  return value === "en" || value === "zh-CN";
}

/** Maps a BCP 47 tag to a supported locale; any Chinese variant uses zh-CN. */
export function resolveLocale(value: unknown): Locale | undefined {
  if (typeof value !== "string" || !/^(en|zh)(?:-[a-z0-9]{1,8})*$/i.test(value)) return undefined;
  return value.toLowerCase().startsWith("zh") ? "zh-CN" : "en";
}

/** The saved choice, else the first supported browser language, else English. */
export function detectLocale(): Locale {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (isLocale(saved)) return saved;
  } catch {
    // Storage may be disabled; fall through to the browser preference.
  }
  const preferred = typeof navigator === "undefined" ? [] : navigator.languages;
  for (const tag of preferred) {
    const locale = resolveLocale(tag);
    if (locale) return locale;
  }
  return DEFAULT_LOCALE;
}

export function persistLocale(locale: Locale): void {
  try {
    localStorage.setItem(STORAGE_KEY, locale);
  } catch {
    // The in-memory choice still applies for this page.
  }
}

export interface CreateI18nOptions {
  /** Locale modules per locale, see `LocaleGlob`. */
  messages: Record<Locale, LocaleGlob>;
  locale?: Locale;
  /** Throw on missing keys instead of warning (tests). */
  strict?: boolean;
}

export function createAppI18n(options: CreateI18nOptions): AppI18n {
  const strictMissing = (locale: string, key: string): string => {
    throw new Error(`missing i18n key "${key}" (${locale})`);
  };
  const i18n = createI18n({
    legacy: false,
    globalInjection: true,
    locale: options.locale ?? detectLocale(),
    fallbackLocale: DEFAULT_LOCALE,
    messages: {
      en: mergeLocaleModules(options.messages.en),
      "zh-CN": mergeLocaleModules(options.messages["zh-CN"]),
    },
    missingWarn: !options.strict,
    fallbackWarn: false,
    ...(options.strict ? { missing: strictMissing } : {}),
  }) as unknown as AppI18n;
  active = i18n;
  return i18n;
}

/** Translation outside components (API error mapping, toasts, document titles). */
export function translate(key: string, params: Record<string, unknown> = {}): string {
  if (!active) return key;
  return active.global.t(key, params);
}

export function hasTranslation(key: string): boolean {
  return active?.global.te(key) ?? false;
}

export function currentLocale(): Locale {
  return active?.global.locale.value ?? DEFAULT_LOCALE;
}

export type { LocaleGlob, LocaleMessages, LocaleModule } from "./messages";
export {
  useFormat,
  formatBytes,
  formatNumber,
  formatDateTime,
  formatRelativeTime,
  formatDuration,
  pickLocalized,
  useLocalized,
  type LocalizedValue,
} from "./format";
