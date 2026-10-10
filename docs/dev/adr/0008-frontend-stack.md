# 0008：前端技术栈

## 背景

现有前端是 Vue 3 + TypeScript + Vite 的单个 SPA，公开页和后台共用一个入口。数据获取、状态管理、表单校验和国际化都是手写实现，API 类型手工维护，测试只有 happy-dom 下的 DOM 测试。前端需要整体重写，允许重新设计视觉。

## 决定

保留 Vue 3 + TypeScript + Vite，并采用：

| 关注点 | 选择 |
| --- | --- |
| 服务端状态 | TanStack Query |
| 客户端状态 | Pinia |
| 国际化 | vue-i18n（中英文 key 完全一致；各模块自带 locale 文件） |
| 组件与样式 | Reka UI + Tailwind CSS v4，颜色等取自设计令牌，浅色与深色主题 |
| 表单校验 | vee-validate + zod（自带适配器；官方 `@vee-validate/zod` 不支持 zod 4） |
| API 类型 | openapi-typescript 生成并提交，openapi-fetch 调用；CI 检查生成物与规范一致 |
| 测试 | Vitest + happy-dom + Testing Library + MSW；Playwright 冒烟测试 |
| 代码规范 | ESLint（typescript-eslint 类型检查规则）+ Prettier |

公开站点和后台是两个 Vite 入口（`index.html`、`admin.html`），公开页不加载后台代码，构建时检查。服务端按规范 `x-spa-routes` 为公开路由返回 `index.html`、为 `/admin/...` 返回 `admin.html`。

## 后果

- 随产物分发的 npm 包要记录在 `third_party/README.md` 并附许可证原文；构建会检查遗漏。
- 规范变化后必须重新生成前端类型，否则门禁失败。
- 服务端的 SPA 白名单与前端路由由 `x-spa-routes` 统一，前端测试检查一致。
