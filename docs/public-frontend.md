# 公共目录与 SPA

公共目录为 `/`，详情为 `/<vendor>/<app>`，目前注册 `openai/codex` 与 `anthropic/claude-code`。详情与后台导航由编译注册表描述，不依赖前端应用名称判断。分发文件在同一应用根下；未知文件与 API 不返回成功 HTML。

Vue Router 支持内部导航、直达、刷新和前后退。公共与后台有独立布局；后台设置、应用版本与事件是明确子路由，未登录进入登录页。服务端对每次管理 API 请求验证 session、CSRF 与请求 origin，路由守卫只负责界面。

`GET /api/bootstrap` 返回版本、平台、双语站点文案、应用公共描述、有效公共 origin 与 revision；不含代理凭据或管理状态。公共 URL 优先级为后台覆盖、`REDAPP_PUBLIC_URL`、通过合法 Host 语法和可信代理链验证的请求 origin。清空后台值只撤销覆盖。旧 `/api/info`、`/apps/...` 和未指定应用的分发入口不再提供。

语言在 mount 前同步选择：手动保存值优先，其次浏览器 languages 中首个支持项；列表不可用/为空才检查 language；最终使用英语。站点自定义文案单独异步加载，局部骨架、失败回退与重试不阻止整个应用使用。首页已删除组织下载副句，保留可配置站点品牌。

Overview 与应用摘要仅在当前页面可见时订阅状态；版本、资源和事件从有上限的独立分页接口读取。设置页不轮询 status。表单各自拥有加载状态、服务器 revision、草稿和错误，离页提示未保存修改，迟到响应不能串入另一应用。

Select 与账户菜单共享弹层基础设施和样式，但分别保留 combobox/listbox 与 menu 的键盘及 ARIA 语义。原 OpenAI SVG 仍以原始字节嵌入，通过 `/openai/codex/icon.svg` 提供；[素材来源说明](../internal/apps/codex/assets/README.md)不因 URL 改动而改变。

本轮验证使用 CLI、类型检查和 DOM 测试，不声称完成截图、真实触摸设备或辅助技术验收。


## 未发布：命令字体与复制反馈

仅 Markdown 渲染的代码块和行内代码使用本地 JetBrains Mono Regular 400 v2.304；正文、原始 HTML 代码和增强边界不变。唯一 WOFF2 文件 92,164 字节（约 90 KiB），未引入斜体/粗体或全字重家族，也未修改上游字体；SHA-256 为 `a9cb1cd82332b23a47e3a1239d25d13c86d16c4220695e34b243effa999f45f2`。

来源为 [JetBrains 官方仓库 v2.304](https://github.com/JetBrains/JetBrainsMono/tree/v2.304)，固定 commit `cd5227bd1f61dff3bbd6c814ceaf7ffd95e947d9` 的 `fonts/webfonts/JetBrainsMono-Regular.woff2`。SIL OFL 1.1 原文随字体打包在 `frontend/public/assets/JetBrainsMono-OFL-v2.304.txt`，构建后嵌入程序并可从同源 `/assets/JetBrainsMono-OFL-v2.304.txt` 读取；字体同源路径为 `/assets/JetBrainsMono-Regular-v2.304.woff2`，无需 CDN 或运行时外网。

[官方字形说明](https://www.jetbrains.com/lp/mono/#design)明确区分 1/l/I，0 带中心点而 O 无中心点；同版本 TTF 的 CLI 字形检查也确认这些字符使用不同轮廓、相同 600 advance（0 三轮廓、O 两轮廓）。这不是 GUI/像素级视觉验收。代码块采用 15px（.9375rem）与 1.65 行高，行内代码随正文缩放；禁用 liga/calt，保留系统等宽及中文 fallback、横向滚动、选择与精确复制。iframe 样式自含并引用同源字体，现有 CSP 允许加载，无需放宽 CSP。

多行块复制成功仅按钮显示勾选及“已复制”，禁用三秒后恢复；下方成功播报保留为脱离布局的屏幕阅读器文本，不产生可见重复或空白行。失败仍显示可见错误并允许重试；各代码块状态独立。原始 HTML 不增加复制控件或应用该字体。


## 未发布 0.7.10：可编辑的指定版本命令

`{{latest_version}}` 是与 `{{base_url}}`、`{{app_path}}` 并列的标量变量，可直接写入默认或自定义 Markdown 的普通代码块/行内代码。版本取当前源最高已知有效版本，不回源；无数据或无法读取时取字面 `<version>`。变量在 Markdown 渲染后单次 HTML 转义插入，DOM 与剪贴板保留 `<version>` 而非实体字符串。未知变量原样保留，不执行表达式，不递归展开值中的变量。

末尾指定版本区保持可编辑的原生 details/summary，默认折叠；无隐藏 marker 或动态整块命令插槽。删除中英文重复引导和无版本说明文字。旧 `install_commands` 仅为现有自定义内容兼容保留，新默认不使用。历史自定义 marker 不解释、不自动改写。
