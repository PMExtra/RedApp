import type { LocaleModule } from "@/shared/i18n";

export default {
  site: {
    source: "RedApp 的 GitHub 仓库",
    version: "v{version}",
    versionPlatform: "v{version}（{platform}）",
    versionUnavailable: "版本信息不可用",
  },
} satisfies LocaleModule;
