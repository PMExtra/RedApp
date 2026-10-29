import type { LocaleModule } from "@/shared/i18n";

export default {
  siteSettingsPage: {
    description:
      "How the public site presents itself. Each section is saved on its own; unsaved sections are kept when you save another.",
  },
  proxySettingsPage: {
    description:
      "The outbound proxy and redirect protection of upstream requests. Vendors and applications inherit them unless they set their own.",
  },
} satisfies LocaleModule;
