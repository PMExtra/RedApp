# 0.7.1 运行变更与升级边界

版本号为 `0.7.1`。发布沿用精确 main 提交完整 CI、版本标签及双架构镜像按不可变 digest 运行验证的门禁；实际结果以对应 Actions 记录为准。部署和 stacks 仓库不在本次变更内。

## 数据与 Provider

SQLite schema 5 只接受当前完整结构或已发布 v0.7.0 的完整 schema 4。后者在独占实例锁下事务升级：保留 UID、revision、source epoch、配置、缓存、事件和指标历史，将原 `general-http` 应用及历史源记录改为 `http-cache`，增加说明与持久文件表。不是旧 key 的运行时别名。相同数字但结构不匹配、v0.6 及更早数据仍在只读预检阶段拒绝，不创建锁或改删旧文件。部署 YAML/JSON schema 仍为 1。

升级前停止旧进程，备份完整数据目录（包括 WAL/SHM 和对象文件）；升级后不要用 v0.7.0 程序打开 schema 5。回退应恢复升级前的整份备份。新实例不带任何业务厂商或应用，升级不删除已有条目。配置文件、环境变量、监听端口、数据卷路径不变，不新增带版本号的卷。

| Provider ID | English / 中文 | 能力 |
| --- | --- | --- |
| `info` | App Info / 应用介绍 | 公共资料、图标、说明，无文件或缓存操作 |
| `hosted` | Hosted Files / 文件托管 | 公共资料与说明，管理员上传和一次性 URL 导入，本地下载 |
| `http-cache` | HTTP Cache / HTTP 缓存 | 公共资料与说明，原有 HTTP 上游缓存、刷新与清理 |
| `codex` | Codex / Codex | 公共资料与说明，原有 Codex 发布协议 |
| `claude-code` | Claude Code / Claude Code | 公共资料与说明，原有 Claude Code 发布协议 |

Info 是所有 Provider 共同组合的内容能力。独立 Info 和 Hosted 不接受 BaseURL、源策略或缓存 TTL，不建立 source client/source rows，不启动下载/缓存采样任务，也无法调用缓存、渠道、版本或清理 API。Codex 与 Claude Code 的安装器、原文、patch、信任材料及验证约束不变。

## 使用说明

`GET/PUT /admin/api/apps/<vendor>/<app>/instructions` 使用 `{en,"zh-CN",revision}`；GET 返回 ETag，PUT 通过 If-Match 或 body revision 校验，首次 revision=0。每种语言最多 12000 个 Unicode 字符，拒绝非文本控制字符。说明拥有独立 revision，编辑不改变源身份。冲突保持草稿，页面离开前保护未保存内容，设置页不轮询。

公共详情按当前语言显示纯文本、保留换行、不解析 HTML/Markdown；它与生成的安装命令分开。内容会改变公共 bootstrap revision。该 DTO 可用于未来导出，本轮没有导出/导入配置功能。

## Hosted 文件

管理员在 Files 页选择上传或一次性 HTTP(S) URL 导入。相对路径不以 `/` 开头，拒绝空段、遍历、反斜杠、百分号、query/fragment 和控制字符。同名默认拒绝；明确选择“替换”携带当前资源 ID，期间资源或应用配置变化会冲突，不能静默覆盖别人提交的文件。

上传流与导入流共用现有全局 writer/reader 容量和最大制品大小；认证后的流式上传读取期限为 5 分钟，普通 API 的短期限不变。先写临时对象、校验实际长度和大小、同步文件，再原子发布数据库索引；失败/取消不公开半成品。替换不打断已打开的下载；新读者看到新完整版本。启动清理仅限已识别的临时/未引用提交对象，已保存文件没有 TTL、stale 或自动清理成员关系。

URL 仅用于这次导入，不保存为源、不会自动刷新，公开下载不会访问该 URL。允许签名 query，拒绝 URL userinfo；错误响应不返回 URL 或凭据。使用现有代理和系统 TLS 验证，5 分钟 HTTP 客户端超时，最多跟随 3 次重定向、禁止 HTTPS 降级，不接受压缩传输。私网 HTTP(S) 和显式端口沿用管理员配置源的权限范围。

API：

- `GET .../files?page=1&limit=25`：文件分页，最大 100。
- `POST .../files?transfer_id=<32位hex>`：multipart 中先提供 `path`、可选 `expected_id`，再提供 `file`；不缓存整份上传。
- `POST .../files/import?transfer_id=<32位hex>`：JSON `{path,url,expected_id?}`。
- `GET/DELETE .../files/transfers/<id>`：当前传输进度/取消，按应用隔离。完成记录不无限保留；取消与发布在同一锁下决定先后，已完成提交不会被取消接口回报为成功取消，也不会撤销完成的文件。取消先阻断提交并关闭上游连接，当前系统调用可能短暂收尾，随后临时对象回收；不存在脱离请求继续导入的后台任务。
- `DELETE .../files/<resource-id>`：仅删除该应用当前资源标识匹配的文件；删除应用后仍可查看/显式清除留存文件，但不能上传/导入。
- `GET /api/apps/<vendor>/<app>/files`：活动 Hosted 应用的公共分页列表。
- `GET/HEAD /<vendor>/<app>/<relative-path>`：只读取本地完整文件，支持 Range/ETag，以附件下载并设置 nosniff/CSP。禁止 query。

## 管理界面与分页

厂商卡片通过服务端分页获取，默认每页 12 个；预览最多 5 个应用，展开后每页 20 个。搜索匹配厂商/应用 ID、中英文名称，先过滤再分页；第六个及以后命中的应用不会被预览折叠隐藏。列表不再展示内部 ID 与全路径。支持 current/disabled/deleted 视图，空目录有创建引导。

应用公共头部集中呈现名称、厂商、图标、Provider、状态及操作。发布类有 Versions and resources、Cache management、Settings；HTTP Cache 有 Cache management、Settings；Hosted 有 Files、Settings；Info 只有 Settings。版本和资源采用桌面左右、窄屏纵向布局，移除重复版本选择器；按需挂载每页任务。历史诊断可折叠，仍保持活动页采样展示。

目录、版本、资源、Hosted 文件使用 `items,page,limit,total,total_pages`；正整数页码可直接跳转。没有结果时 page/total_pages=1，总数=0；数据减少时服务端夹到末页。前端只保留当前页，切应用、过滤条件和搜索会取消旧请求，迟到结果不能覆盖新状态。`page` 与 `cursor` 不可并用；事件及缓存维护预览继续使用已有游标接口。

语言与账户菜单共享弹出层外观，分别保留 listbox/menu 语义和键盘行为；不引入 UI 框架。旧管理导航与 UI 路由不保留兼容别名，登录后返回受验证的站内目标。

## 验证与部署影响

Go race/vet、CLI HTTP/数据目录、前端类型/DOM/非 UTC 与静态嵌入构建覆盖此变更；最终通过情况以交付验证报告与日志为准。本地隔离容器使用无网络、只读根、非 root 用户及独立临时卷。发布还要求原生 Linux amd64/arm64 和 Windows PowerShell 7/5.1 CI，以及发布镜像的双架构 digest 运行验证。不进行 GUI/截图或用户部署。

stacks 仅需在未来获准升级时选择新镜像；配置字段、端口、卷挂载、healthcheck 和启动命令不变。上传大文件时，已有反向代理的请求体上限/超时可能需要管理员按实际文件大小调整，本轮不修改 stacks 或代理。0.8 规划保持独立，不实现新源认证、私有 CA、签名配置、自动镜像同步或完整配置导出。
