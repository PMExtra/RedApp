import type { LocaleModule } from "@/shared/i18n";

export default {
  settings: {
    discard: "Discard changes",
    site: {
      title: "Site texts",
      description:
        "Public title, subtitle and footer notice for each language. Plain text only; the project link always points to RedApp.",
      fields: {
        title: "Site title",
        subtitle: "Subtitle",
        disclaimer: "Footer notice",
        disclaimerHint: "Shown in the footer of every page. Up to {max} characters.",
      },
      titleRequired: "Enter a title.",
      controlCharacters: "Remove control characters; only line breaks and tabs are allowed.",
      save: "Save site texts",
      saved: "Site texts saved.",
    },
    publicUrl: {
      title: "Public URL",
      description:
        "The address clients use to reach this service, used in install commands and links.",
      effective: "Effective URL",
      environment: "REDAPP_PUBLIC_URL",
      noEnvironment: "Not set",
      sources: {
        override: "From the override",
        environment: "From REDAPP_PUBLIC_URL",
        request: "From this request",
      },
      requestHint:
        "Without an override or REDAPP_PUBLIC_URL, links use the address of each request. Set an override if clients use a different address.",
      override: "Override",
      overrideHint:
        "An http or https origin, such as https://downloads.example.com, without a path. Leave empty to use REDAPP_PUBLIC_URL or the request address. Saving does not contact the address.",
      invalid: "Enter an http or https address without user name, path, query or fragment.",
      clear: "Clear override",
      save: "Save public URL",
      saved: "Public URL saved.",
    },
    homepage: {
      title: "Pinned applications",
      description:
        "Applications shown first on the public home page, in this order. Disabled applications stay pinned but are hidden from the public.",
      listLabel: "Pinned applications in display order",
      empty: "No pinned applications. The home page shows only the most downloaded ones.",
      add: "Add an application",
      addHint: "Search by name, ID or tag. {count} of {max} pinned.",
      full: "The limit of {max} pinned applications is reached. Remove one to add another.",
      searchPlaceholder: "Search applications…",
      alreadyPinned: "Pinned",
      remove: "Remove {name}",
      added: "{name} added at the end.",
      removed: "{name} removed.",
      save: "Save pinned applications",
      saved: "Pinned applications saved.",
    },
    proxy: {
      title: "Global upstream proxy",
      description:
        "Used for metadata, downloads and imports unless a vendor or application sets its own. Environment proxy variables are ignored.",
      urlRequired: "Enter the proxy URL.",
      urlScheme: "Use an http://, https:// or socks5:// URL.",
      dns: {
        local: "Host names are resolved by this server.",
        proxy: "Host names are resolved by the proxy (SOCKS5).",
      },
      direct: "Upstream connections are made directly, without a proxy.",
      save: "Save proxy",
      saved: "Proxy saved. New upstream connections use it; transfers in progress continue.",
    },
  },
} satisfies LocaleModule;
