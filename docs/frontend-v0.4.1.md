# v0.4.1 站点设置与交互修订

本轮修订延续 v0.4.0 的 Vue/Go 单进程结构与全部认证、缓存和安装器边界，不引入前端组件库或外部运行服务。

## 十项要求与实现

| 要求 | 行为与主要实现 |
| --- | --- |
| 双语默认副标题 | 英文 `Application Redistribution Platform`，中文 `应用再分发平台`；默认标题均为 RedApp。后端 `internal/site/defaults.json` 与前端 `src/site.ts` 保持一致 |
| 管理员编辑站点文案 | 设置页同时编辑 English / 简体中文的标题、副标题和声明；成功保存才更新当前页面，迟到的初始化响应不覆盖已保存值 |
| 公共导航对齐 | 顶栏、正文、页脚共用 1200px 宽度和响应式水平留白，长标题允许换行 |
| 通用声明 | 移除单个应用的隶属声明，页脚统一显示面向所有应用开发者的声明；管理员可分别编辑两种语言 |
| 页脚架构分工 | 公共页显示版本，不显示架构；后台显示 `v0.4.1 (linux/amd64)` 形式，无 Architecture/架构标签 |
| 复制按钮 | 仅安全上下文且存在 Clipboard API 时显示；权限拒绝等实际失败使用可访问的行内状态提示，命令始终可选择复制 |
| 终端用户安装页 | 保留命令、必要操作及可信服务/哈希/登录提示；脚本审查、出口策略和操作系统上线验证放入管理员文档 |
| 统一下拉框 | `SelectMenu.vue` 替换语言、版本筛选、代理凭据及历史计数视图四处原生 select；语言显示地球图标和 English / 简体中文 |
| 流量语义 | 回源按已读取的 identity HTTP 响应体计数，分发按外部压缩前写入接受的字节计数；修复写盘/长度失败前漏计，不改变 43 项指标键 |
| 固定项目链接 | 两端页脚的 RedApp 始终链接 `https://github.com/PMExtra/RedApp`，不受自定义站点标题影响 |

## 设置契约与兼容

`GET /admin/api/site` 和带会话、同源及 CSRF 校验的 `POST /admin/api/site` 使用完整 JSON 对象：`title`、`subtitle`、`disclaimer`，每项均有 `en` 与 `zh-CN` 字符串。标题每种语言必填，最多 80 个 Unicode 字符；副标题最多 160，声明最多 500，后两者可以为空。去除首尾空白，拒绝不合法 UTF-8 及换行/制表符之外的控制字符。前端输入框的长度限制按浏览器规则，含补充平面字符时可能更严格。

设置写入既有 SQLite `records` 的 `setting/site`，不增加 schema 迁移。没有记录的旧数据目录直接使用默认值，旧记录缺少字段时补默认值；无效持久记录返回错误，避免掩盖损坏。保存操作覆盖整个文案对象，多管理员同时编辑时以后一次成功保存为准。已有页面不后台轮询站点文案，重新打开或刷新会取得新值。

公开的 `/api/info` 增加 `site` 对象，保留 version/os/arch；前台只隐藏架构显示，不把公开构建信息当成机密。公共响应无会话 Cookie 并使用 no-store。所有文案只以 Vue 文本插值显示，不支持 HTML、Markdown、脚本或自定义链接；项目链接为代码中的固定地址。

## 下拉框与键盘

参考 [W3C APG select-only combobox](https://www.w3.org/WAI/ARIA/apg/patterns/combobox/examples/combobox-select-only/) 的交互与 ARIA 约定，并参考 [React Aria Select](https://react-aria.adobe.com/Select) 的列表选择模式；未引入其依赖或复制其实现。

DOM 焦点保持在带 combobox 角色的按钮，列表使用 listbox/option 与唯一 ID，`aria-expanded`、`aria-controls`、`aria-activedescendant` 表达状态。方向键、Home/End、PageUp/PageDown 和按字母查找移动活动项；Enter/空格确认，Tab 确认后正常移动焦点，Escape 取消。点击外部关闭，失焦提交活动项；禁用或空列表不能打开。活动项与已提交项分别突出显示，展开后滚动活动项进入列表可视区域。不把 DOM 模拟测试视为真实读屏或触屏兼容性证明。

## 流量与验收边界

压缩、Range、重试、共享下载、失败字节、速率和历史数据的精确定义见[指标说明](metrics-history.md#制品流量与压缩口径)。本轮不启用 HTTP gzip，也不改变压缩安装包的字节表示；保留偏移续传和 SHA256 对同一表示的校验。

实际命令与结果统一记录在[验收文档](acceptance.md#v041-站点设置交互与流量口径)。本轮 Go race、Vue DOM、构建和 HTTP 集成验证，以及 Chromium 中英桌面/窄屏交互和代表截图审阅均通过；公共顶栏/正文/页脚边距偏差不超过 1px。真实读屏、真实移动设备及其它浏览器仍需独立验证；镜像运行结果以本版本发布工作流为准，不沿用 v0.4.0 的结果。
