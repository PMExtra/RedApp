import type { LocaleModule } from "@/shared/i18n";

export default {
  configuration: {
    reset: "Restore the template value",
    resetTo: "Restore the template value: {value}",
    unsaved: "Unsaved changes",
    discard: "Discard changes",
    saved: "Changes saved.",
    nothingToSave: "There are no changes to save.",
    templateMissing:
      "The built-in template of this item is no longer available; the last accepted template values stay in use.",
    linkedHint:
      "Built-in item: fields follow the template until you change them. Use the restore button next to a field to follow the template again.",
  },
} satisfies LocaleModule;
