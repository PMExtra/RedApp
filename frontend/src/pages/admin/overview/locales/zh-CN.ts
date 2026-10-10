import type { LocaleModule } from "@/shared/i18n";

export default {
  overviewPage: {
    description: "此 RedApp 服务的全局指标，每 5 秒刷新一次。",
    sampled: "采样于",
    started: "服务启动于",
    stale: "刷新失败，显示的是上一次成功的快照。",
  },
} satisfies LocaleModule;
