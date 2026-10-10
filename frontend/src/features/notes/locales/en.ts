import type { LocaleModule } from "@/shared/i18n";

export default {
  notes: {
    title: "Admin notes",
    description:
      "Private maintenance notes for administrators. They are never shown on public pages.",
    label: "Notes",
    count: "{count} of {max} characters",
    tooLong: "At most {max} characters.",
    control: "Remove control characters (only tabs and line breaks are allowed).",
    save: "Save notes",
    saved: "Notes saved.",
    unsaved: "Unsaved changes",
    discard: "Discard changes",
  },
} satisfies LocaleModule;
