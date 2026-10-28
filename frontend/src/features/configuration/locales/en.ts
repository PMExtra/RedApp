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
    followUpstream: "Follow the upstream value",
    followUpstreamValue: "Follow the upstream value: {value}",
    upstreamMissing:
      "The upstream no longer publishes this application; the last values received from it stay in use.",
    upstreamHint:
      "Mirrored application: the name, description, icon, usage instructions and directory listings follow the upstream unless you change them here. The source always comes from the upstream.",
    linkedHint:
      "Linked template: local values take precedence. Provider-specific settings inherit only when providers match. Restore buttons appear only for compatible template fields.",
  },
} satisfies LocaleModule;
