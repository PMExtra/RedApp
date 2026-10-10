import type { Schema } from "@/shared/api";

/** The vendor logo for a UI language: the language logo, else the default logo. */
export function vendorLogo(
  vendor: Pick<Schema<"Vendor">, "icon" | "localized_icons">,
  locale: string,
): string {
  const localizedIcon =
    locale === "zh-CN" ? vendor.localized_icons["zh-CN"] : vendor.localized_icons.en;
  return localizedIcon || vendor.icon;
}
