import type { LocaleModule } from "@/shared/i18n";

export default {
  proxy: {
    legend: "上游代理",
    modes: {
      inherit: "继承",
      direct: "直接连接",
      url: "代理 URL",
    },
    url: "代理 URL",
    urlHint:
      "支持 http、https 或 socks5，必须写明端口。已保存的密码显示为 ****，保留即可沿用原密码。",
    effective: "生效：{target}（来自{scope}）",
    scopes: {
      app: "应用",
      vendor: "厂商",
      global: "全局设置",
    },
  },
} satisfies LocaleModule;
