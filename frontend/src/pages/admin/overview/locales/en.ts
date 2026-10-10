import type { LocaleModule } from "@/shared/i18n";

export default {
  overviewPage: {
    description: "Global metrics of this RedApp service, refreshed every 5 seconds.",
    sampled: "Sampled",
    started: "Service started",
    stale: "Refresh failed. Showing the last successful snapshot.",
  },
} satisfies LocaleModule;
