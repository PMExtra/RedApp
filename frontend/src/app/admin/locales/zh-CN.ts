import type { LocaleModule } from "@/shared/i18n";

export default {
  adminShell: {
    title: "管理后台",
    badge: "管理",
    openMenu: "打开导航",
    nav: {
      overview: "概览",
      events: "事件",
      directory: "目录",
      vendors: "厂商与应用",
      categories: "分类",
      settings: "设置",
      site: "站点",
      proxy: "上游代理",
    },
    titles: {
      overview: "概览",
      events: "事件",
      site: "站点设置",
      proxy: "上游代理",
      vendors: "厂商",
      vendorNew: "新建厂商",
      vendor: "厂商",
      appNew: "新建应用",
      app: "应用",
      categories: "分类",
      notFound: "页面不存在",
    },
    tabs: {
      label: "分区",
      vendorSettings: "设置",
      vendorApps: "应用",
      adminNotes: "管理员备注",
      appSettings: "设置",
      versions: "版本",
      cache: "缓存",
      files: "文件",
    },
    notFound: {
      description: "该管理页面不存在。",
      back: "前往概览",
    },
  },
} satisfies LocaleModule;
