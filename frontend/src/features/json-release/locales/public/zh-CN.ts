import type { LocaleModule } from "@/shared/i18n";
export default {
  releaseDownloads: {
    title: "下载",
    download: "下载",
    more: "其他安装包",
    version: "版本 {version}",
    retry: "重试",
    unavailable: "暂无可用安装包",
    stale: "可能已过期",
    os: { windows: "Windows", macos: "macOS", linux: "Linux", unknown: "未知系统" },
  },
  releaseVersions: {
    choose: "版本 {version}；选择其他版本",
    loading: "正在加载版本…",
    unavailable: "无法获取版本列表。",
    truncated: "仅列出最新的 200 个版本。",
  },
} satisfies LocaleModule;
