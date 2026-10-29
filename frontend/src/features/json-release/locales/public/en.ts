import type { LocaleModule } from "@/shared/i18n";
export default {
  releaseDownloads: {
    title: "Download",
    download: "Download",
    more: "Other packages",
    version: "Version {version}",
    retry: "Retry",
    unavailable: "No packages available",
    stale: "May be outdated",
    os: { windows: "Windows", macos: "macOS", linux: "Linux", unknown: "Unknown OS" },
  },
  releaseVersions: {
    choose: "Version {version}; choose another version",
    loading: "Loading versions…",
    unavailable: "Versions are unavailable.",
    truncated: "Only the newest 200 versions are listed.",
  },
} satisfies LocaleModule;
