import type { LocaleModule } from "@/shared/i18n";
export default {
  downloadChoices: {
    title: "Download choices",
    description: "What the public page offers. Hidden packages stay downloadable by their links.",
    default: "Default package",
    defaultHint:
      "Recommended when the visitor's system or architecture is unknown or has no package of its own.",
    noDefault: "None (the first package)",
    offered: "Offered packages",
    offeredHint:
      "Packages of the current releases. Packages added later are offered until you clear them here.",
    none: "No release has been discovered yet; packages appear after the first refresh.",
    missing: "{key} (not in the current releases)",
    saved: "Download choices saved",
  },
  jsonRelease: {
    prewarm: "Packages to prewarm after metadata refresh",
    applyPrewarm: "Use selected prewarm keys",
    prewarmHint:
      "Selection uses stable package keys. Save the configuration to apply it; an empty selection disables prewarm for this channel.",

    title: "JSON release discovery",
    mirrored:
      "The upstream RedApp provides this configuration: a mirror reads the releases and downloads from the upstream. Stop mirroring to edit it.",
    config: "Request and query configuration (JSON)",
    trust:
      "Queries are trusted administrator configuration, not a sandbox. Intermediate memory is not hard-limited. Preview fetches metadata and checksum sidecars only.",
    preview: "Preview",
    channel: "Preview channel",
    invalid: "Enter a JSON configuration object.",
    result: "Normalized preview",
    saved: "Release configuration saved",
    createHint:
      "Provide request matrices, a pure jq query, authorized source boundaries and exact prewarm keys. See the JSON release guide.",
  },
} satisfies LocaleModule;
