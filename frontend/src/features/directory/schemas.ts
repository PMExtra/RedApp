import { z } from "zod";
import { formError, utf8Length } from "@/shared/forms";
import { isSlug, isVendorId } from "@/shared/lib";

/*
 * Client-side rules mirroring the spec (`Name`, `Description`, `HttpUrl`,
 * `ProxyConfig`, IDs). The server validates again; these catch typos early.
 */

const nameText = z
  .string()
  .refine((value) => value.trim() !== "", formError("directory.validation.nameRequired"))
  .refine((value) => utf8Length(value) <= 256, formError("directory.validation.nameTooLong"));

const descriptionText = z
  .string()
  .refine(
    (value) => utf8Length(value) <= 16384,
    formError("directory.validation.descriptionTooLong"),
  );

export const nameSchema = z.object({ en: nameText, "zh-CN": nameText });
export const descriptionSchema = z.object({ en: descriptionText, "zh-CN": descriptionText });

/** Absolute http(s) URL without credentials, query or fragment. */
export function isHttpUrl(value: string): boolean {
  if (!/^https?:\/\//i.test(value) || value.length > 4096) return false;
  try {
    const url = new URL(value);
    return !url.username && !url.password && !url.search && !url.hash && url.hostname !== "";
  } catch {
    return false;
  }
}

export const httpUrlSchema = z
  .string()
  .refine((value) => isHttpUrl(value.trim()), formError("directory.validation.url"));

export const baseUrlsSchema = z
  .array(httpUrlSchema)
  .min(1, formError("directory.validation.urlsCount"))
  .max(16, formError("directory.validation.urlsCount"))
  .refine(
    (urls) => new Set(urls.map((url) => url.trim())).size === urls.length,
    formError("directory.validation.urlsUnique"),
  );

export const strategySchema = z.enum(["ordered", "round_robin", "random"]);

export const ttlSchema = z
  .number({ error: formError("directory.validation.ttl") })
  .int(formError("directory.validation.ttl"))
  .min(0, formError("directory.validation.ttl"))
  .max(86400, formError("directory.validation.ttl"));

export const proxySchema = z
  .object({ mode: z.enum(["inherit", "direct", "url"]), url: z.string().optional() })
  .superRefine((value, context) => {
    if (value.mode === "url" && !value.url?.trim()) {
      context.addIssue({
        code: "custom",
        path: ["url"],
        message: formError("directory.validation.proxyUrl"),
      });
    }
  });

export const iconSchema = z.string();

export const vendorIdSchema = z
  .string()
  .refine((value) => isVendorId(value), formError("directory.validation.vendorId"));

export const appIdSchema = z
  .string()
  .refine((value) => isSlug(value), formError("directory.validation.appId"));
