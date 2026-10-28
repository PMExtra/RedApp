import type { LocaleModule } from "@/shared/i18n";

export default {
  fileIndex: {
    title: "目录列表",
    description: "允许访客按目录浏览此应用的文件。",
    templateMissing: "模板已不可用，继续使用上次接受的默认值。",
    label: "列出目录",
    hint: "在应用地址或目录地址末尾加 / 即以网页列出其中的文件；请求带 Accept: application/json 时返回 JSON。HTTP 缓存的列表来自源站，包含尚未缓存的文件。",
    saved: "目录列表设置已保存。",
  },
} satisfies LocaleModule;
