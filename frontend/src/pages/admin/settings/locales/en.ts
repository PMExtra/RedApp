import type { LocaleModule } from "@/shared/i18n";

export default {
  siteSettingsPage: {
    description:
      "How the public site presents itself. Each section is saved on its own; unsaved sections are kept when you save another.",
  },
  proxySettingsPage: {
    description:
      "The outbound proxy for upstream requests. Vendors and applications inherit it unless they set their own.",
  },
} satisfies LocaleModule;
