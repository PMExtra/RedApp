import type { LocaleModule } from "@/shared/i18n";

export default {
  events: {
    caption: "最近的运行事件，最新在前",
    empty: "最近 30 天没有事件。",
    columns: {
      time: "时间",
      event: "事件",
      app: "应用",
      resource: "资源",
      message: "消息",
    },
    httpStatus: "HTTP {status}",
    version: "版本 {version}",
    generation: "代次 {id}",
  },
} satisfies LocaleModule;
