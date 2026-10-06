import type { Application } from "./bootstrap";
import type { LocalizedText } from "./site";
import { language } from "./i18n";

export function vendorName(vendor: { id: string; name?: Partial<LocalizedText> } | undefined, fallbackID = "", publisher = ""): string {
  return vendor?.name?.[language.value]?.trim() || vendor?.name?.en?.trim() || publisher.trim() || vendor?.id || fallbackID;
}
export function applicationVendorName(app: Application): string {
  return vendorName(app.vendor, app.id.split("/")[0], app.publisher);
}
