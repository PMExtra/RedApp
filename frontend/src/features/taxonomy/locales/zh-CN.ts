import type { LocaleModule } from "@/shared/i18n";

export default {
  taxonomy: {
    title: "分类与标签",
    description: "分类用于在公开站点上归类应用；标签是仅管理员可见的搜索关键词。",
    categories: {
      label: "分类",
      selected: "已选分类",
      placeholder: "搜索分类或输入新名称",
      hint: "选择已有分类，或输入新名称后按回车。新分类在保存时创建。",
      pendingLabel: "{name}（新）",
      ambiguous: "此名称与多个分类相同，请从列表中选择。",
      tooLong: "分类名称最多 {max} 个字符。",
      tooMany: "一个应用最多 {max} 个分类。",
      loadFailed: "无法加载分类列表，目前只能输入新名称。",
    },
    tags: {
      label: "标签",
      new: "新标签",
      placeholder: "添加标签",
      add: "添加标签",
      hint: "按回车添加。开头的 # 会被忽略；标签不会显示在公开页面上。",
      tooLong: "标签最多 {max} 个字符。",
      tooMany: "一个应用最多 {max} 个标签。",
    },
    page: {
      description: "分类在编辑应用时创建，无人使用时自动删除。此处可以修改分类名称。",
      search: "搜索分类",
      caption: "分类",
      name: "名称",
      id: "ID",
      otherName: "其他语言",
      applications: "应用数",
      actions: "操作",
      builtin: "内置",
      rename: "重命名 {name}",
      empty: "还没有分类。可在编辑应用时添加。",
      noMatches: "没有匹配的分类。",
    },
    edit: {
      title: "重命名分类 {id}",
      description: "分类名称不能与其他分类重复，并会显示在公开站点上。",
      nameIn: "名称（{language}）",
      nameRequired: "请输入名称。",
      nameTooLong: "最多 256 字节。",
    },
  },
} satisfies LocaleModule;
