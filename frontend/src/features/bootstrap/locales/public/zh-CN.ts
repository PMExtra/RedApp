import type { LocaleModule } from "@/shared/i18n";

export default {
  site: {
    source: "源代码",
    version: "v{version}",
    versionPlatform: "v{version}（{platform}）",
    versionUnavailable: "版本信息不可用",
  },
} satisfies LocaleModule;
