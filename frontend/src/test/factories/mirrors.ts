/*
 * Mirror fixtures: upstream instances, their catalogs, mirror states and the
 * configuration of a mirrored application.
 */
import type { Schema } from "@/shared/api";
import { app } from "./directory";
import { localized } from "./index";
import { httpCacheConfiguration } from "./runtime";

type Overrides<T> = Partial<T>;
type CatalogItem = Schema<"UpstreamCatalogPage">["items"][number];

export const UPSTREAM_UID = "0a1b2c3d4e5f60718293a4b5c6d7e8f9";

export function upstream(overrides: Overrides<Schema<"Upstream">> = {}): Schema<"Upstream"> {
  return {
    uid: UPSTREAM_UID,
    name: "DMZ",
    origin: "https://dmz.example",
    instance_id: "f".repeat(32),
    revision: 1,
    created_at: "2026-10-01T08:00:00Z",
    mirrors: 2,
    ...overrides,
  };
}

/** A supported, not yet mirrored upstream application. */
export function upstreamCatalogItem(overrides: Overrides<CatalogItem> = {}): CatalogItem {
  return {
    key: "acme/files",
    provider: "hosted",
    name: localized("Files", "文件"),
    description: localized("Shared files"),
    icon: "/assets/icons/acme-files.svg",
    supported: true,
    mirror: null,
    ...overrides,
  };
}

export function appMirror(overrides: Overrides<Schema<"AppMirror">> = {}): Schema<"AppMirror"> {
  return {
    upstream: upstream(),
    remote_key: "acme/files",
    remote_revision: "7",
    upstream_chain: ["f".repeat(32)],
    available: true,
    synced_at: "2026-10-28T08:00:00Z",
    checked_at: "2026-10-28T09:00:00Z",
    error: null,
    ...overrides,
  };
}

/** The `http-cache` application mirroring the upstream's hosted `acme/files`. */
export function mirroredApp(overrides: Overrides<Schema<"App">> = {}): Schema<"App"> {
  return app({
    id: "files",
    vendor_id: "mirror",
    key: "mirror/files",
    name: localized("Files", "文件"),
    base_urls: ["https://dmz.example/acme/files"],
    ...overrides,
  });
}

/** Its configuration: display fields follow the upstream, the source is the upstream's. */
export function mirroredConfiguration(
  overrides: Overrides<Schema<"AppConfiguration">> = {},
): Schema<"AppConfiguration"> {
  const base = httpCacheConfiguration();
  const defaults = {
    ...base.effective,
    name: localized("Files", "文件"),
    description: localized("Shared files"),
    base_urls: ["https://dmz.example/acme/files"],
  };
  return httpCacheConfiguration(
    {},
    {
      revision: 9,
      template_ref: `upstream:${mirroredApp().uid}`,
      template_hash: "d".repeat(64),
      template_provider: "http-cache",
      defaults,
      effective: { ...defaults, description: localized("Local description") },
      fields: {
        "name.en": { source: "inherited", differs_from_template: false },
        "description.en": { source: "custom", differs_from_template: true },
      },
      ...overrides,
    },
  );
}
