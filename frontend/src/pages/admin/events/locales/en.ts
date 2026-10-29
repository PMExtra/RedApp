import type { LocaleModule } from "@/shared/i18n";

export default {
  eventsPage: {
    description:
      "Failures and warnings from downloads, metadata and cleanup. The newest 1000 events of the last 30 days are kept.",
  },
  appEventsPage: {
    description:
      "Failures and warnings of this application, such as refused redirects and failed downloads. Events of the last 30 days are kept.",
  },
} satisfies LocaleModule;
