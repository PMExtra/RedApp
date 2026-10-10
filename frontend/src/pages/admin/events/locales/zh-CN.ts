import type { LocaleModule } from "@/shared/i18n";

export default {
  eventsPage: {
    description: "下载、元数据和清理中的失败与警告。保留最近 30 天内最新的 1000 条事件。",
    stale: "刷新失败，显示的是上一次成功加载的页面。",
    cursorExpired: "此页已不可用。",
    firstPage: "回到第一页",
  },
} satisfies LocaleModule;
