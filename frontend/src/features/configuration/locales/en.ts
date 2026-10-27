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
    templateProvider: "Template provider",
    linkedHint:
      "Linked template: local values take precedence. Provider-specific settings inherit only when providers match. Restore buttons appear only for compatible template fields.",
  },
} satisfies LocaleModule;
