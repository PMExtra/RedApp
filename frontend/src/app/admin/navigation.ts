import { Activity, Building2, LayoutDashboard, Network, Settings, Tags } from "@lucide/vue";
import type { SideNavSection } from "@/shared/ui";

type Translate = (key: string) => string;

/** Side navigation of the admin shell; labels are i18n keys resolved at render. */
export function adminNavigation(t: Translate): SideNavSection[] {
  return [
    {
      items: [
        {
          label: t("adminShell.nav.overview"),
          to: { name: "admin-overview" },
          icon: LayoutDashboard,
        },
        { label: t("adminShell.nav.events"), to: { name: "admin-events" }, icon: Activity },
      ],
    },
    {
      label: t("adminShell.nav.directory"),
      items: [
        { label: t("adminShell.nav.vendors"), to: { name: "admin-vendors" }, icon: Building2 },
        { label: t("adminShell.nav.categories"), to: { name: "admin-categories" }, icon: Tags },
      ],
    },
    {
      label: t("adminShell.nav.settings"),
      items: [
        { label: t("adminShell.nav.site"), to: { name: "admin-settings-site" }, icon: Settings },
        { label: t("adminShell.nav.proxy"), to: { name: "admin-settings-proxy" }, icon: Network },
      ],
    },
  ];
}
