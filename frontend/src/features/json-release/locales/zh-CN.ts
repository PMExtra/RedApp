import type { LocaleModule } from "@/shared/i18n";
export default {
  jsonRelease: {
    prewarm: "元数据刷新后预热的安装包",
    applyPrewarm: "使用选定的预热 key",
    prewarmHint: "选择使用稳定安装包 key。保存配置后生效；空选择关闭该渠道的预热。",

    title: "JSON 发布发现",
    config: "请求与查询配置（JSON）",
    trust:
      "表达式属于可信管理员配置，不是沙箱；中间内存没有硬限制。预览仅获取元数据和校验侧车文件。",
    preview: "解析预览",
    channel: "预览渠道",
    invalid: "请输入 JSON 配置对象。",
    result: "规范化预览结果",
    saved: "发布配置已保存",
    createHint: "配置请求矩阵、纯 jq 查询、授权来源边界与精确预热 key，详见 JSON 发布指南。",
  },
} satisfies LocaleModule;
