# v0.6.0：多应用架构与指标整合

v0.6.0 将 Codex CLI 和 Claude Code 的应用身份统一为 `openai/codex`、`anthropic/claude-code`，共用注册表、catalog、下载引擎和后台 SPA。协议、固定上游与信任根仍由受审查代码定义，不支持运行时插件或任意 URL 注册。

这是破坏性升级。旧数据目录、配置接口和公共路径不能直接沿用；以下说明取代旧版文档中的兼容升级步骤。

## 数据和部署变化

1. 保留旧版本和完整旧数据目录备份，不删除或原地修改旧目录。
2. 从 [v0.6.0 的 config/example.json](https://github.com/PMExtra/RedApp/blob/v0.6.0/config/example.json) 创建显式 JSON 配置，为 `data_dir` 指定全新空目录，配置 `allowed_hosts`；反向代理场景还需明确 `trusted_proxies`。
3. 运行 `redapp config validate --config /etc/redapp/config.json`。验证本身不打开或创建数据目录。
4. 使用新数据卷启动 v0.6.0，配置只读挂载；首次启动重新初始化管理员身份，再在后台设置站点、代理、PUBLIC_URL 和应用 TTL。
5. 重新配置下游安装链接并验证所需平台。缓存按需重新填充，历史从新实例运行时开始采集。

schema 3 有 15 张表，版本、metadata、资源、blob、代际、清理和指标通过规范应用 ID 隔离；全局设置和应用设置的 scope 显式区分。旧版配置、缓存和历史全部不导入。启动只读检查并拒绝旧版或未知目录，不创建新锁/SQLite sidecar 去修改被拒目录；新格式目录可以正常重启。

启动方式为 `redapp serve --config /etc/redapp/config.json`，健康检查读取同一配置。旧 `--data` 等逐字段选项、`REDAPP_DATA`、`REDAPP_LISTEN` 和上游覆盖变量不能替代配置文件。更完整的参数和限额见[运维说明](operations.md)。降级应停止新实例并重新使用保留的旧版本及其旧目录，不能把 schema 3 数据交给旧程序。

## 路由和后台

公开详情与分发根为 `/<vendor>/<app>`：例如 `/openai/codex/install.sh`、`/anthropic/claude-code/install.ps1`。旧 `/apps/codex`、`/apps/claude-code`、顶层 `/install.sh` 等路径不作兼容别名。后台使用 `/admin/api/apps/<vendor>/<app>/...` 显式选择应用，省略身份不再默认为 Codex。

SPA 页面分别拥有表单草稿、请求和错误；只刷新当前可见的数据页。版本、资源和事件使用有界分页。切换应用立即清空 TTL 等旧草稿，旧请求迟到不能覆盖当前应用；状态轮询不清除设置保存错误。共享弹出层保留 menu 和 listbox 各自的键盘语义。语言在 mount 前按手动选择、浏览器语言列表、可用的单语言 fallback、English 的顺序确定。

PUBLIC_URL 的优先级为后台持久化覆盖、`REDAPP_PUBLIC_URL`、经信任规则验证的请求 origin。清除覆盖发送 `null`，可恢复环境默认。后台修改不会扩大入站 Host 或上游授权范围。设置携带 revision，CAS 冲突保留用户草稿，不静默覆盖其他管理员的修改。

## 指标和安装器

全局活动指标为 41 项：16 项常用直接显示，25 项诊断默认折叠；折叠不停止刷新、采样或历史查询。全局版本数统计所有应用的版本对，同名版本分别计数；应用页只统计当前应用。历史默认 7 天，可切换 24 小时和 30 天；读数显示浏览器本地时间和偏移、精确基础单位，区分 gauge/rate 的 value/avg/min/max 与 counter 的 last/delta、缺失点和已观测零。UTC 分桶保持不变，键盘和触摸事件可选择数据点。

`counters.reuse_requests` 停止独立写入、采样和展示；`events.recent_total` 停止采样和展示，错误事件详情保留。退役定义只供已有观测查询和聚合，历史按原始 24 小时/小时 30 天规则自然过期，不按 active 清单强删。新实例不导入旧历史，也不存在旧版 Codex 口径迁移 marker。

18 份原始安装器、patch、生成脚本、provenance、许可和信任文件迁至规范目录后保持字节一致。每日维护从统一 descriptor 读取官方来源、路径、验证器和允许修改集合；只有真实上游变化通过严格 patch、隔离验证和产物审查后才允许创建草稿 PR，不自动合并或放宽信任边界。

## 验证和发布边界

本地恢复与整合记录见[验证报告](multi-application-validation.md)，它记录的是发布前本地检查，不冒称托管 CI 或发布结果。发布门禁要求精确 main 提交通过 Linux amd64/arm64 的类型/DOM、Go race/vet、CLI/HTTP、完整 Dockerfile 和隔离运行检查，以及 Windows PowerShell 7/5.1 的安装器回归；只有通过后才创建版本标签并发布 GHCR `0.6.0`、`0.6`、`latest`（保留既有 `v0.6.0` 镜像别名）。双架构发布后按 manifest 和各架构 digest 验证版本、健康、持久化、实例锁及崩溃恢复。

本轮不进行 GUI/截图、用户部署或 stacks 修改，不删除旧数据目录。安装器 CI 使用本地确定性数据和无害测试程序，验证参数、摘要失败保护、入口、环境和退出码，不执行真实 Claude 二进制；不能将它等同于真实 Claude 运行、自动更新控制、认证/API 网络或生产负载验收。macOS 实机及其他原生运行边界仍须独立确认。TLS 和签名校验始终保留。
