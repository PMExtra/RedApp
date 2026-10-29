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
  redirectProtection: {
    legend: "重定向保护",
    modes: {
      inherit: "继承",
      strict: "严格保护",
      basic: "基础保护",
      off: "关闭",
    },
    hints: {
      strict:
        "上游跳转只能到达已配置的来源：发布类应用的来源，或 HTTP 缓存的源地址与重定向白名单。同时执行基础保护的检查。",
      basic:
        "允许跳转到任意目标，但跳到其他主机必须使用 HTTPS，不能从 HTTPS 降级为 HTTP，公网来源不能跳到内网地址。",
      off: "不做任何检查直接跟随跳转。仅用于可信的来源。",
    },
    effective: "生效：{mode}（来自{scope}）",
  },
} satisfies LocaleModule;
