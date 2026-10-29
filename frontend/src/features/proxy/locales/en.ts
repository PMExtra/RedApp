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
  redirectProtection: {
    legend: "Redirect protection",
    modes: {
      inherit: "Inherit",
      strict: "Strict",
      basic: "Basic",
      off: "Off",
    },
    hints: {
      strict:
        "Upstream redirects may only reach the configured sources: release sources, or an HTTP cache's base URLs and redirect allowlist. The basic checks also apply.",
      basic:
        "Any target is allowed, but a redirect to another host must use HTTPS, HTTPS never downgrades to HTTP and a public source cannot redirect into the intranet.",
      off: "Redirects are followed without any check. Use this only for trusted sources.",
    },
    effective: "Effective: {mode} (from {scope})",
  },
} satisfies LocaleModule;
