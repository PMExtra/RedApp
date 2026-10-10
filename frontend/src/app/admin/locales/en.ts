import type { LocaleModule } from "@/shared/i18n";

export default {
  adminShell: {
    title: "Administration",
    badge: "Admin",
    openMenu: "Open navigation",
    nav: {
      overview: "Overview",
      events: "Events",
      directory: "Directory",
      vendors: "Vendors and applications",
      categories: "Categories",
      settings: "Settings",
      site: "Site",
      proxy: "Upstream proxy",
    },
    titles: {
      overview: "Overview",
      events: "Events",
      site: "Site settings",
      proxy: "Upstream proxy",
      vendors: "Vendors",
      vendorNew: "New vendor",
      vendor: "Vendor",
      appNew: "New application",
      app: "Application",
      categories: "Categories",
      notFound: "Page not found",
    },
    tabs: {
      label: "Sections",
      vendorSettings: "Settings",
      vendorApps: "Applications",
      adminNotes: "Admin notes",
      appSettings: "Settings",
      versions: "Versions",
      cache: "Cache",
      files: "Files",
    },
    notFound: {
      description: "This administration page does not exist.",
      back: "Go to the overview",
    },
  },
} satisfies LocaleModule;
