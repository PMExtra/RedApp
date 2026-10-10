import type { LocaleModule } from "@/shared/i18n";

export default {
  site: {
    source: "Source code",
    version: "v{version}",
    versionPlatform: "v{version} ({platform})",
    versionUnavailable: "Version unavailable",
  },
} satisfies LocaleModule;
