# 公共目录与 SPA

公共目录为 `/`，详情为 `/<vendor>/<app>`，目前注册 `openai/codex` 与 `anthropic/claude-code`。详情与后台导航由编译注册表描述，不依赖前端应用名称判断。分发文件在同一应用根下；未知文件与 API 不返回成功 HTML。

Vue Router 支持内部导航、直达、刷新和前后退。公共与后台有独立布局；后台设置、应用版本与事件是明确子路由，未登录进入登录页。服务端对每次管理 API 请求验证 session、CSRF 与请求 origin，路由守卫只负责界面。

`GET /api/bootstrap` 返回版本、平台、双语站点文案、应用公共描述、有效公共 origin 与 revision；不含代理凭据或管理状态。公共 URL 优先级为后台覆盖、`REDAPP_PUBLIC_URL`、通过合法 Host 语法和可信代理链验证的请求 origin。清空后台值只撤销覆盖。旧 `/api/info`、`/apps/...` 和未指定应用的分发入口不再提供。

语言在 mount 前同步选择：手动保存值优先，其次浏览器 languages 中首个支持项；列表不可用/为空才检查 language；最终使用英语。站点自定义文案单独异步加载，局部骨架、失败回退与重试不阻止整个应用使用。首页已删除组织下载副句，保留可配置站点品牌。

Overview 与应用摘要仅在当前页面可见时订阅状态；版本、资源和事件从有上限的独立分页接口读取。设置页不轮询 status。表单各自拥有加载状态、服务器 revision、草稿和错误，离页提示未保存修改，迟到响应不能串入另一应用。

Select 与账户菜单共享弹层基础设施和样式，但分别保留 combobox/listbox 与 menu 的键盘及 ARIA 语义。原 OpenAI SVG 仍以原始字节嵌入，通过 `/openai/codex/icon.svg` 提供；[素材来源说明](../internal/apps/codex/assets/README.md)不因 URL 改动而改变。

本轮验证使用 CLI、类型检查和 DOM 测试，不声称完成截图、真实触摸设备或辅助技术验收。
