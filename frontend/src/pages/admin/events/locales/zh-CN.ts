import type { LocaleModule } from "@/shared/i18n";

export default {
  eventsPage: {
    description: "下载、元数据和清理中的失败与警告。保留最近 30 天内最新的 1000 条事件。",
  },
  appEventsPage: {
    description: "此应用的失败与警告，例如被拦截的跳转和失败的下载。保留最近 30 天的事件。",
  },
} satisfies LocaleModule;
