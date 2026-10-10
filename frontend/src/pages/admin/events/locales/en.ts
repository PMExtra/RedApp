import type { LocaleModule } from "@/shared/i18n";

export default {
  eventsPage: {
    description:
      "Failures and warnings from downloads, metadata and cleanup. The newest 1000 events of the last 30 days are kept.",
    stale: "Refresh failed. Showing the last successful page.",
    cursorExpired: "This page is no longer available.",
    firstPage: "Go to the first page",
  },
} satisfies LocaleModule;
