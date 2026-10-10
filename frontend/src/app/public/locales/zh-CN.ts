import type { LocaleModule } from "@/shared/i18n";

export default {
  publicShell: {
    nav: {
      all: "全部应用",
      admin: "管理后台",
    },
    siteUnavailable: "站点信息不可用。",
    titles: {
      home: "首页",
      catalog: "全部应用",
      vendor: "厂商",
      app: "应用",
      notFound: "页面不存在",
    },
    notFound: {
      title: "页面不存在",
      description: "该页面不存在或已不再公开。",
      home: "返回首页",
    },
  },
} satisfies LocaleModule;
