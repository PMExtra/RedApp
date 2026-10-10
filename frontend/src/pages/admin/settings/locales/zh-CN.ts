import type { LocaleModule } from "@/shared/i18n";

export default {
  siteSettingsPage: {
    description:
      "公开站点的展示方式。每个区块单独保存；保存一个区块时，其他区块未保存的内容会保留。",
  },
  proxySettingsPage: {
    description: "上游请求使用的出站代理。厂商和应用默认继承此设置，除非另行配置。",
  },
} satisfies LocaleModule;
