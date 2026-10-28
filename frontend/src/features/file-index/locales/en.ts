import type { LocaleModule } from "@/shared/i18n";

export default {
  fileIndex: {
    title: "Directory listings",
    description: "Let visitors browse the files of this application by directory.",
    templateMissing: "The template is no longer available; the last accepted defaults stay in use.",
    label: "List directories",
    hint: "Adding / to the end of the application address or of a directory shows its files as a page, or as JSON when requested with Accept: application/json. HTTP cache listings come from the origin, including files not cached yet.",
    saved: "Directory listings saved.",
  },
} satisfies LocaleModule;
