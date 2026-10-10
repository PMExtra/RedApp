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
    linkedHint: "内置项：字段在修改前跟随模板。点击字段旁的恢复按钮可重新跟随模板。",
  },
} satisfies LocaleModule;
