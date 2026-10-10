import type { LocaleModule } from "@/shared/i18n";

export default {
  session: {
    login: {
      title: "登录",
      description: "请输入管理员密码。",
      password: "密码",
      passwordRequired: "请输入密码。",
      submit: "登录",
      signingIn: "正在登录…",
    },
    notices: {
      signedOut: "已退出登录。",
      passwordChanged: "密码已修改，请使用新密码登录。",
      expired: "会话已过期，请重新登录。",
    },
    expired: {
      title: "会话已过期",
      description: "请重新登录以继续。此页面上未保存的更改会保留。",
    },
    checkFailed: "无法检查登录状态。",
    account: {
      menu: "管理员",
      changePassword: "修改密码",
      publicSite: "公开站点",
      signOut: "退出登录",
    },
    password: {
      title: "修改密码",
      description: "修改后所有会话（包括当前会话）都会退出登录。",
      current: "当前密码",
      new: "新密码",
      confirm: "确认新密码",
      lengthRule: "长度为 12 到 72 字节。",
      mismatch: "两次输入的密码不一致。",
      submit: "修改密码",
    },
  },
} satisfies LocaleModule;
