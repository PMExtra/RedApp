import type { LocaleModule } from "@/shared/i18n";

export default {
  catalog: {
    home: {
      title: "Applications",
      browseAll: "Browse all applications",
      pinned: "Featured",
      popular: "Popular this week",
      popularEmpty: "No downloads in the last 7 days yet.",
    },
    list: {
      title: "All applications",
      search: "Search applications",
      placeholder: "Search by name or ID",
      results: "No applications | 1 application | {n} applications",
      noMatches: "No applications match “{q}”.",
      clearSearch: "Clear search",
      empty: "No applications are published yet.",
      vendorEmpty: "This vendor has no published applications yet.",
      pageEmpty: "This page is empty.",
      firstPage: "Go to the first page",
    },
    categories: {
      label: "Categories",
      chip: "{name} ({count})",
      all: "All",
    },
    app: {
      latestVersion: "Latest version",
      firstSeen: "first seen {time}",
      versionUnknown: "No version seen yet",
      instructions: "Usage instructions",
    },
    hosted: {
      title: "Downloads",
      count: "{n} file | {n} files",
      empty: "No files have been published yet.",
      pageEmpty: "This page has no files.",
    },
    prefix: {
      title: "Download URL prefix",
      description:
        "Append a file's relative path to this address. Files are fetched and cached on first request.",
      unavailable: "The public address is unavailable right now.",
    },
  },
} satisfies LocaleModule;
