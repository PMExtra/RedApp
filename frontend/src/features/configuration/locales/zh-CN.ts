import type { LocaleModule } from "@/shared/i18n";

export default {
  configuration: {
    reset: "恢复模板值",
    resetTo: "恢复模板值：{value}",
    unsaved: "有未保存的更改",
    discard: "放弃更改",
    saved: "更改已保存。",
    nothingToSave: "没有需要保存的更改。",
    templateMissing: "此项关联的内置模板已不可用，将继续使用最后接受的模板值。",
    templateProvider: "模板提供程序",
    linkedHint:
      "已关联模板：本地设置优先；专属配置仅在提供程序一致时继承。只有兼容的模板字段显示恢复按钮。",
  },
} satisfies LocaleModule;
