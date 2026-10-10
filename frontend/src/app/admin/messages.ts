import type { LocaleGlob } from "@/shared/i18n";

/** Locale modules of the admin entry: everything, including features' public texts. */
export const adminMessages = {
  en: import.meta.glob<LocaleGlob[string]>(
    [
      "/src/shared/**/locales/en.ts",
      "/src/app/admin/locales/en.ts",
      "/src/pages/admin/**/locales/en.ts",
      "/src/features/*/locales/en.ts",
      "/src/features/*/locales/public/en.ts",
    ],
    { eager: true, import: "default" },
  ),
  "zh-CN": import.meta.glob<LocaleGlob[string]>(
    [
      "/src/shared/**/locales/zh-CN.ts",
      "/src/app/admin/locales/zh-CN.ts",
      "/src/pages/admin/**/locales/zh-CN.ts",
      "/src/features/*/locales/zh-CN.ts",
      "/src/features/*/locales/public/zh-CN.ts",
    ],
    { eager: true, import: "default" },
  ),
};
