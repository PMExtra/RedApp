import type { LocaleModule } from "@/shared/i18n";

export default {
  settings: {
    discard: "放弃更改",
    site: {
      title: "站点文本",
      description: "各语言的公开标题、副标题和页脚声明。仅支持纯文本；项目链接始终指向 RedApp。",
      fields: {
        title: "站点标题",
        subtitle: "副标题",
        disclaimer: "页脚声明",
        disclaimerHint: "显示在每个页面的页脚，最多 {max} 个字符。",
      },
      titleRequired: "请输入标题。",
      controlCharacters: "请删除控制字符；只允许换行和制表符。",
      save: "保存站点文本",
      saved: "站点文本已保存。",
    },
    publicUrl: {
      title: "公开地址",
      description: "客户端访问此服务所用的地址，用于安装命令和链接。",
      effective: "生效地址",
      environment: "REDAPP_PUBLIC_URL",
      noEnvironment: "未设置",
      sources: {
        override: "来自覆盖设置",
        environment: "来自 REDAPP_PUBLIC_URL",
        request: "来自当前请求",
      },
      requestHint:
        "未设置覆盖地址和 REDAPP_PUBLIC_URL 时，链接使用每个请求的地址。如果客户端使用其他地址访问，请设置覆盖地址。",
      override: "覆盖地址",
      overrideHint:
        "http 或 https 源地址，例如 https://downloads.example.com，不含路径。留空则使用 REDAPP_PUBLIC_URL 或请求地址。保存时不会访问该地址。",
      invalid: "请输入不含用户名、路径、查询参数或片段的 http 或 https 地址。",
      clear: "清除覆盖地址",
      save: "保存公开地址",
      saved: "公开地址已保存。",
    },
    homepage: {
      title: "置顶应用",
      description:
        "按此顺序优先显示在公开首页的应用。已停用或已删除的应用仍保留置顶，但在移除或重新发布之前不会公开显示。",
      listLabel: "按显示顺序排列的置顶应用",
      empty: "没有置顶应用。首页只显示下载最多的应用。",
      add: "添加应用",
      addHint: "按名称、ID 或标签搜索。已置顶 {count} / {max} 个。",
      full: "已达到 {max} 个置顶应用的上限。移除一个后才能添加。",
      searchPlaceholder: "搜索应用…",
      alreadyPinned: "已置顶",
      remove: "移除 {name}",
      added: "已将 {name} 添加到末尾。",
      removed: "已移除 {name}。",
      save: "保存置顶应用",
      saved: "置顶应用已保存。",
      states: {
        disabled: "已停用，不显示",
        deleted: "已删除，不显示",
        missing: "已不存在",
      },
    },
    proxy: {
      title: "全局上游代理",
      description: "用于元数据、下载和导入，除非厂商或应用另有设置。忽略环境变量中的代理设置。",
      urlRequired: "请输入代理 URL。",
      urlScheme: "请使用 http://、https:// 或 socks5:// 开头的 URL。",
      dns: {
        local: "主机名由本服务器解析。",
        proxy: "主机名由代理解析（SOCKS5）。",
      },
      direct: "上游连接直接建立，不使用代理。",
      save: "保存代理",
      saved: "代理已保存。新的上游连接将使用它；进行中的传输不受影响。",
    },
  },
} satisfies LocaleModule;
