import { afterEach, expect, it } from "vitest";
import { setLanguage } from "./i18n";
import { applicationVendorName, vendorName } from "./vendorName";
import { applications } from "./testSupport";
afterEach(() => setLanguage("en"));
it("uses current vendor locale with English, legacy publisher and ID fallbacks", () => {
  const vendor = { id: "vendor", name: { en: "English vendor", "zh-CN": "中文厂商" } };
  setLanguage("zh-CN");
  expect(vendorName(vendor)).toBe("中文厂商");
  expect(vendorName({ ...vendor, name: { en: "English vendor" } })).toBe("English vendor");
  expect(vendorName({ id: "vendor" }, "fallback", "Legacy")).toBe("Legacy");
  expect(vendorName({ id: "vendor" })).toBe("vendor");
  expect(vendorName(undefined, "fallback")).toBe("fallback");
  expect(applicationVendorName({ ...applications[0]!, vendor })).toBe("中文厂商");
  setLanguage("en");
  expect(vendorName(vendor)).toBe("English vendor");
});
