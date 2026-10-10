import type { LocaleModule } from "@/shared/i18n";

export default {
  proxy: {
    legend: "Upstream proxy",
    modes: {
      inherit: "Inherit",
      direct: "Direct connection",
      url: "Proxy URL",
    },
    url: "Proxy URL",
    urlHint:
      "http, https or socks5 with an explicit port. A saved password is shown as ****; leave it to keep the password.",
    effective: "Effective: {target} (from {scope})",
    scopes: {
      app: "application",
      vendor: "vendor",
      global: "global setting",
    },
  },
} satisfies LocaleModule;
