import type { LocaleGlob } from "@/shared/i18n";

/*
 * Locale modules of the public entry: shared, the public shell and pages,
 * and the `locales/public/` part of each feature. Admin-only texts never
 * reach public visitors.
 */
export const publicMessages = {
  en: import.meta.glob<LocaleGlob[string]>(
    [
      "/src/shared/**/locales/en.ts",
      "/src/app/public/locales/en.ts",
      "/src/pages/public/**/locales/en.ts",
      "/src/features/*/locales/public/en.ts",
    ],
    { eager: true, import: "default" },
  ),
  "zh-CN": import.meta.glob<LocaleGlob[string]>(
    [
      "/src/shared/**/locales/zh-CN.ts",
      "/src/app/public/locales/zh-CN.ts",
      "/src/pages/public/**/locales/zh-CN.ts",
      "/src/features/*/locales/public/zh-CN.ts",
    ],
    { eager: true, import: "default" },
  ),
};
