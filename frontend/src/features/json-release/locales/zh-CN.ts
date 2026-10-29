import type { LocaleModule } from "@/shared/i18n";
export default {
  releaseRefetch: {
    action: "重新获取",
    title: "重新获取版本 {version}？",
    description:
      "适用于上游为这个版本重新出包的情况。它的缓存文件会被删除，然后通过渠道或按版本查找重新发现这个版本；文件在需要时重新下载。",
    confirm: "重新获取",
    done: "已重新获取版本 {version}",
  },
  releaseLookup: {
    title: "发现版本",
    description:
      "从上游查找较早的版本。发现的版本会被保存，不改变任何渠道，并出现在版本列表和公开页面中。",
    version: "版本",
    submit: "发现",
    found: "已发现版本 {version}",
    unavailable:
      "发布配置没有 lookup。在 JSON 发布配置中添加 lookup 后即可按版本发现，见 JSON 发布指南。",
  },
  downloadChoices: {
    title: "下载选项",
    description: "决定公开页面提供哪些安装包。隐藏的安装包仍可通过链接下载。",
    default: "默认安装包",
    defaultHint: "访问者的系统或架构无法识别，或没有对应安装包时推荐。",
    noDefault: "无（使用第一个安装包）",
    offered: "提供的安装包",
    offeredHint: "当前发布中的安装包。以后新增的安装包默认提供，可在此取消。",
    none: "尚未发现任何发布；首次刷新后会列出安装包。",
    missing: "{key}（当前发布中没有）",
    saved: "下载选项已保存",
  },
  jsonRelease: {
    prewarm: "元数据刷新后预热的安装包",
    applyPrewarm: "使用选定的预热 key",
    prewarmHint: "选择使用稳定安装包 key。保存配置后生效；空选择关闭该渠道的预热。",

    mirrored:
      "此配置由上游 RedApp 提供：镜像从上游读取版本信息并下载文件。如需编辑，请先停止镜像。",
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
