# 0008：前端技术栈

## 背景

现有前端是 Vue 3 + TypeScript + Vite 的单个 SPA，公开页和后台共用一个入口。数据获取、状态管理、表单校验和国际化都是手写实现，API 类型手工维护，测试只有 happy-dom 下的 DOM 测试。阶段 4 将重写前端，允许重新设计视觉。

## 决定

保留 Vue 3 + TypeScript + Vite，并采用：

| 关注点 | 选择 |
| --- | --- |
| 服务端状态 | TanStack Query |
| 客户端状态 | Pinia |
| 国际化 | vue-i18n（中英文 key 完全一致） |
| 组件与样式 | Reka UI + Tailwind CSS |
| 表单校验 | vee-validate + zod |
| API 类型 | 由 OpenAPI 规范生成（阶段 3 产出） |
| 测试 | Vitest + Testing Library + MSW；Playwright 冒烟测试 |
| 代码规范 | ESLint + Prettier |

公开站点和后台使用独立入口，公开页不加载后台代码。

## 后果

- 新增依赖要更新 `third_party/README.md` 和许可证原文。
- API 类型依赖阶段 3 的 OpenAPI 规范，所以前端重写排在其后。
- 重写完成之前，现有前端只做必要修复。
