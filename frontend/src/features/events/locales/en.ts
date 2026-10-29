import type { LocaleModule } from "@/shared/i18n";

export default {
  events: {
    caption: "Recent operational events, newest first",
    empty: "No events in the last 30 days.",
    columns: {
      time: "Time",
      event: "Event",
      app: "Application",
      resource: "Resource",
      message: "Message",
    },
    httpStatus: "HTTP {status}",
    version: "Version {version}",
    generation: "Generation {id}",
    stale: "Refresh failed. Showing the last successful page.",
    cursorExpired: "This page is no longer available.",
    firstPage: "Go to the first page",
    messageTitle: "Event message",
    showMessage: "Show the full message: {message}",
  },
} satisfies LocaleModule;
