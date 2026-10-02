import { computed, ref } from "vue";
import { language } from "./i18n";
export type LocalizedText = { en: string; "zh-CN": string };
export type SiteSettings = {
  title: LocalizedText;
  subtitle: LocalizedText;
  disclaimer: LocalizedText;
};
export const defaultSite: SiteSettings = {
  title: { en: "RedApp", "zh-CN": "RedApp" },
  subtitle: {
    en: "Application Redistribution Platform",
    "zh-CN": "应用再分发平台",
  },
  disclaimer: {
    en: "Independent distribution service. Not affiliated with or endorsed by the developers of the distributed applications.",
    "zh-CN": "独立分发服务，与所分发应用的开发商无隶属关系，也未获得其背书。",
  },
};
export const siteSettings = ref<SiteSettings>(structuredClone(defaultSite));
export const siteTitle = computed(
  () => siteSettings.value.title[language.value],
);
export const siteRevision = ref(0);
export function applySite(value: unknown, expectedRevision?: number) {
  if (expectedRevision !== undefined && expectedRevision !== siteRevision.value)
    return;
  const candidate = value as SiteSettings | undefined;
  if (
    !candidate ||
    ![candidate.title, candidate.subtitle, candidate.disclaimer].every(
      (text) =>
        text &&
        typeof text.en === "string" &&
        typeof text["zh-CN"] === "string",
    )
  )
    return;
  siteSettings.value = structuredClone(candidate);
  siteRevision.value++;
}
