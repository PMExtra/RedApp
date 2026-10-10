import type { LocaleModule } from "@/shared/i18n";

export default {
  catalog: {
    home: {
      title: "应用",
      browseAll: "浏览全部应用",
      pinned: "精选",
      popular: "本周热门",
      popularEmpty: "最近 7 天还没有下载。",
    },
    list: {
      title: "全部应用",
      search: "搜索应用",
      placeholder: "按名称或 ID 搜索",
      results: "没有应用 | 1 个应用 | {n} 个应用",
      noMatches: "没有与“{q}”匹配的应用。",
      clearSearch: "清除搜索",
      empty: "还没有发布任何应用。",
      vendorEmpty: "该厂商还没有发布应用。",
      pageEmpty: "此页没有内容。",
      firstPage: "回到第一页",
    },
    categories: {
      label: "分类",
      chip: "{name}（{count}）",
      all: "全部",
    },
    app: {
      latestVersion: "最新版本",
      firstSeen: "首次发现于{time}",
      versionUnknown: "尚未发现版本",
      instructions: "使用说明",
    },
    hosted: {
      title: "下载",
      count: "{n} 个文件 | {n} 个文件",
      empty: "还没有发布任何文件。",
      pageEmpty: "此页没有文件。",
    },
    prefix: {
      title: "下载地址前缀",
      description: "在此地址后追加文件的相对路径即可下载。文件在首次请求时获取并缓存。",
      unavailable: "暂时无法获取公开地址。",
    },
  },
} satisfies LocaleModule;
