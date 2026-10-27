import type { LocaleModule } from "@/shared/i18n";
export default {
  jsonRelease: {
    prewarm: "Packages to prewarm after metadata refresh",
    applyPrewarm: "Use selected prewarm keys",
    prewarmHint:
      "Selection uses stable package keys. Save the configuration to apply it; an empty selection disables prewarm for this channel.",

    title: "JSON release discovery",
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
