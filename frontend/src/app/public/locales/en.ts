import type { LocaleModule } from "@/shared/i18n";

export default {
  publicShell: {
    nav: {
      all: "All applications",
      admin: "Administration",
    },
    siteUnavailable: "Site information is unavailable.",
    titles: {
      home: "Home",
      catalog: "All applications",
      vendor: "Vendor",
      app: "Application",
      notFound: "Page not found",
    },
    notFound: {
      title: "Page not found",
      description: "The page does not exist or is no longer published.",
      home: "Back to the home page",
    },
  },
} satisfies LocaleModule;
