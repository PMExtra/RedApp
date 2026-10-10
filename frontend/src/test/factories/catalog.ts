/* Fixtures of the public catalog (package A). */
import type { Schema } from "@/shared/api";
import { localized, publicApp, publicVendor } from "./index";

type Overrides<T> = Partial<T>;

/** A published application of another provider, keyed `vendor/id`. */
export function catalogApp(
  key: string,
  overrides: Overrides<Schema<"PublicApp">> = {},
): Schema<"PublicApp"> {
  const [vendor = "acme", id = "app"] = key.split("/");
  return publicApp({
    key,
    id,
    vendor: publicVendor({ id: vendor, name: localized(vendor.toUpperCase()) }),
    name: localized(`App ${id}`),
    description: localized(`Description of ${id}.`, `${id} 的说明。`),
    ...overrides,
  });
}

/** Capabilities of an `http-cache` application (files, no installers or versions). */
export const httpCacheCapabilities: Schema<"Capabilities"> = {
  details: true,
  instructions: true,
  hosted_files: false,
  files: true,
  versions: false,
  installers: false,
  time_cleanup: true,
};

/** Capabilities of a `hosted` application. */
export const hostedCapabilities: Schema<"Capabilities"> = {
  details: true,
  instructions: true,
  hosted_files: true,
  files: true,
  versions: false,
  installers: false,
  time_cleanup: false,
};

export function categoryCount(
  id: string,
  count: number,
  name = localized(id.charAt(0).toUpperCase() + id.slice(1)),
): Schema<"CategoryCount"> {
  return { id, name, count };
}

/** One catalog page; `total` defaults to the item count. */
export function catalogPage(
  overrides: Overrides<Schema<"CatalogPage">> = {},
): Schema<"CatalogPage"> {
  const items = overrides.items ?? [publicApp()];
  const limit = overrides.limit ?? 24;
  const total = overrides.total ?? items.length;
  return {
    items,
    page: 1,
    limit,
    total,
    total_pages: Math.max(1, Math.ceil(total / limit)),
    categories: [],
    ...overrides,
  };
}

export function hostedFile(path: string, overrides: Overrides<Schema<"HostedFile">> = {}) {
  return {
    id: `01J${path
      .replace(/[^A-Za-z0-9]/g, "")
      .toUpperCase()
      .padEnd(23, "0")
      .slice(0, 23)}`,
    path,
    sha256: "a".repeat(64),
    size_bytes: 1536,
    created_at: "2026-10-01T08:00:00Z",
    ...overrides,
  } satisfies Schema<"HostedFile">;
}

export function hostedFilePage(
  overrides: Overrides<Schema<"HostedFilePage">> = {},
): Schema<"HostedFilePage"> {
  const items = overrides.items ?? [hostedFile("setup.exe")];
  const limit = overrides.limit ?? 25;
  const total = overrides.total ?? items.length;
  return {
    items,
    page: 1,
    limit,
    total,
    total_pages: Math.max(1, Math.ceil(total / limit)),
    ...overrides,
  };
}
