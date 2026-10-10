import type { LocaleModule } from "@/shared/i18n";

export default {
  notes: {
    title: "管理员备注",
    description: "仅管理员可见的维护备注，不会显示在公开页面上。",
    label: "备注",
    count: "{count} / {max} 个字符",
    tooLong: "最多 {max} 个字符。",
    control: "请删除控制字符（只允许制表符和换行）。",
    save: "保存备注",
    saved: "备注已保存。",
    unsaved: "有未保存的更改",
    discard: "放弃更改",
  },
} satisfies LocaleModule;
