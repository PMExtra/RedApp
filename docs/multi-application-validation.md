# 通用多应用重构：本地实施与验证记录

本记录对应新环境本地分支 `recovery-multiapp-metrics`，基线为 `88c789e023597774a1ca1b812887b46b59a3f49c`（v0.5.0）。2026-10-02 从已核验源码补丁恢复核心，再选择性适配指标前端；没有重写架构、导入旧数据或使用旧编译产物。默认工作树保持基线且干净，所有恢复、构建及回归仅在新的独立工作树进行。

用户批准的临时 Git 交接提交为 `db89325991ece578c229dd880f4fc770300756e3`，只读取其中的源码数据，没有合并该提交或 `.handoff` 文件。消费者本地验证交接 JSON 542596 bytes / SHA256 `5b335e7707fb77ee1d262ca81ee3eb1beaa7c0df0674ecd876bd3929c77f4b28`；限定文件名和解压大小后验证：

- `core.patch`：1314697 bytes / `6862a7cca560c578cbe56bf2b252c8e7c63e9de8046eb8320ddf28a2f219d9bb`。
- `metrics-frontend.patch`：48275 bytes / `836847cf6c3b276061fe76d5075b4b8e2ff66d9f876a15cd7664c771a22cb1ff`。

验证后按明确授权删除唯一远端临时分支 `handoff/multiapp-recovery-20261002-2227`，`ls-remote` 确认不存在。原始输入另存本地备份。此次未推送产品提交、打 tag、创建 release 或部署；二进制为 `dev`，未指定新版本号。此前 Library 源材料下载失败没有被冒称成功，后续恢复依据上述新授权的 Git 交接。

## 实施范围

- 编译期 manifest、注册表和 Protocol adapter 代替 HTTP、catalog、前端对两个应用的分支判断。规范 ID 为 `openai/codex`、`anthropic/claude-code`；上游、信任根和协议仍由受审查代码决定，不支持运行时插件或任意 URL 注册。
- catalog 统一渠道 TTL、并发合流、metadata 持久化、不可变资源授权与错误边界。复用既有协议的 `example/third` 契约 fixture 验证第三应用无需改共享下载/存储内核。
- SQLite schema 3 使用 15 张表；全局与应用设置有显式 scope、revision 和 CAS。资源身份为 `(app_id, version, resource_key)`，完整 blob 按 `(app_id, sha256)` 复用，跨应用不共享物理文件。
- 随机 generation、读写租约、续传、验证发布、恢复及清理冻结快照贯穿同一下载内核。清理报告逻辑容量、预计可回收完整 blob 和活动 generation，避免同应用复用产生物理容量重复计数。
- 部署只接受显式 JSON 配置。PUBLIC_URL 的优先级是后台覆盖、`REDAPP_PUBLIC_URL`、安全请求 origin；修改链接来源不扩大入站 Host 或上游授权范围。覆盖清空为 `null`，失败和 CAS 冲突不改变运行配置。
- 配置、缓存、历史都不从旧版导入。启动在创建实例锁和写入前只读拒绝旧版/未知目录；新格式目录可重启。旧目录保留，不自动升级、清空或删除。
- 公开详情和分发根统一为 `/<vendor>/<app>`。Vue Router 支持公共/后台布局和深层页面；设置页无 status 轮询，草稿、revision 和错误由每个表单管理。locale 在 mount 前同步确定。
- 状态 API 返回摘要。版本、资源和事件通过应用明确、最多 100 项的分页接口读取；资源筛选、游标和迟到响应不会跨应用复用。
- installer 清单、每日维护 inventory 和发布允许集合来自同一 manifest。现有 18 份原文/patch/generated/许可/信任文件迁至规范目录后与基线逐字节相同；自动更新不能修改 patch、注册表、信任根或验证器。

## 原评审问题的处理

| 原问题 | 本地实现与回归 |
| --- | --- |
| 切换应用后 TTL GET 失败可能提交上一个应用的值 | 切换立即清空草稿和 revision；失败时不能保存；迟到结果按请求序号和应用隔离 |
| 全局 versions.total 只计 Codex | 按注册应用分别 COUNT 再求和；同名版本属于两应用时计 2，应用历史保留独立 scope |
| 全局 status 成功刷新清除设置失败消息 | 设置不参与 status timer，各请求拥有独立错误和取消生命周期 |
| 降低制品大小上限后旧缓存仍可分发 | 完整命中、应用内 blob 复用、片段和 metadata 已知大小均执行限额；拒绝分发但不删除缓存 |
| 同应用共享坏 blob 无法在线修复 | 退休受影响旧引用、旧 reader 明确失败，以完整校验的新文件原子替换；其他应用隔离 |

## 指标整合边界

后端已停止 `counters.reuse_requests` 独立写入/采样和 `events.recent_total` 采样；active 目录为 41，完整历史 Catalog 为 43，两个退役定义仅用于已有样本查询/聚合。事件详情保留，历史按原 raw 24h/hour 30d 周期自然过期，不按 active 名单强删未知历史。新架构不导入历史，因此不增加口径迁移表。

前端已完成 16 项常用/25 项默认折叠诊断分组；隐藏退役卡片但保留错误详情。诊断折叠不停止刷新、后台采样或历史查询。增强读数包含本地时间和时区偏移、原始精度与基础单位、gauge/rate 的 value 或 avg/min/max、counter 的 last/delta、缺失值与覆盖信息；保留键盘和触摸事件支持。

旧指标补丁带有旧库 Codex 历史迁移 marker 的类型、提示和测试。新架构不导入旧历史，因此没有保留这些旧假设；版本指标明确区分全局和当前应用。沿用新 SPA 的显式应用 API、独立表单及错误生命周期；历史作用域变化会取消旧请求、清空旧选点并丢弃迟到结果。嵌入资源已在本环境由源码重新生成。

## 验证记录与边界

本次实际工具链为 Go 1.27.1、Node 24.19.0、npm 11.9.0；官方 Go 下载包按公布 SHA256 校验，npm 使用可写缓存并遵守原 lockfile。下列结果来自此次新环境；恢复材料中的旧记录不替代本次证据。首轮 Go 检查曾与 Vite 输出替换重叠而缺少嵌入文件，已在前端构建完成后重新完整执行并通过。

| 检查 | 当前结果 |
| --- | --- |
| 整仓 `go test -race ./...` | 全部通过；涵盖下载、存储、历史、HTTP、协议、认证和配置；未报告 data race |
| `go build ./...`、`go vet ./...`、gofmt、diff 检查 | 通过；静态 Linux amd64 二进制为 `RedApp dev (commit 88c789e-dirty)` |
| SPA typecheck、DOM、生产嵌入构建 | 19 文件 51 项 DOM 与类型检查通过；UTC 及 Asia/Shanghai 全套通过，America/New_York 的历史/i18n 22 项通过；生产嵌入构建通过 |
| 真实进程 HTTP/数据 CLI | 最终二进制通过；验证启动、规范/深层路径、健康、登录、CAS、PUBLIC_URL/installer 即时更新、分页限额、摘要不带列表、SIGTERM、重启、拒绝旧/未知目录且内容不变 |
| 下载故障与并发 | race 覆盖 100 followers、reader/writer 上限、跨应用隔离、同应用 blob 复用、坏 blob 修复、限额、清理排空、旧快照/新 generation、DB/磁盘失败、SIGKILL 与恢复 |
| installer | Codex 25、Claude Shell 64 离线场景及更新器保护通过；维护 16 场景通过，真实本地 HTTP installer 成功/损坏拒绝通过 |
| Docker | 离线 runtime 重建通过；禁用网络、只读根、UID/GID 65532、新空命名卷、重建持久性、healthcheck、双实例拒绝、SIGKILL 与正常停止通过；测试容器和卷清理 |
| 指标关键行为 | 全局/应用历史 API、折叠诊断刷新、切应用关闭旧对话框、取消/迟到响应、tooltip 精度/缺失点/键盘/触摸事件通过；两个退役键拒绝新观测，旧小时聚合保留并按 30 天自然过期通过 |
| 完整 Dockerfile | 未通过环境联网门禁：镜像构建容器先出现 DNS 错误；使用当前环境代理并解析代理地址后，`go mod download` 被代理证书链的 `unknown authority` 拒绝。未关闭 TLS 校验，未修改产品 Dockerfile 规避；本地源码构建与禁网 runtime 回归已通过，不冒称完整多阶段构建通过 |
| 字节身份 | 18 份迁移的 installer/许可/信任文件与固定基线完全相同 |

未验证真实生产负载、全部原生平台或官方交互二进制、Windows/PowerShell/macOS 实机、GUI、触摸/屏幕阅读器体验、daily 有变化分支的真实 GitHub PR 写权限。进程中断/fault injection 测试不等于断电或文件系统硬件持久性保证。没有将未运行的旧 headless 脚本作为新 SPA 门禁。

## 第三应用扩展

1. 在 `internal/apps/builtin/manifest.json` 增加规范身份、公开描述、固定上游、渠道、信任 revision、installer 和受审查 validator。身份不能使用保留 vendor。
2. 协议相同则复用已有 factory；协议不同则实现 `application.Protocol` 并在 `internal/apps/builtin/registry.go` 注册。固定公钥/签名验证与官方 metadata 语义留在 adapter。
3. 在 `installers/<vendor>/<app>/` 增加原文、严格 patch、generated、provenance、许可和必要固定信任文件；有特殊安装行为时增加受审查 validator。品牌静态资源需显式注册。
4. 增加可信 metadata、路径、签名、授权、安装器失败关闭及三应用隔离 fixture。运行共享 catalog/download/store/API 契约及 installer inventory/发布允许集合回归。
5. 重建二进制和前端嵌入资源。通常不改 schema、下载/清理内核、通用路由或组件，也不改变已存在规范 ID；本次从旧版切换的不兼容性不等于以后添加第三应用需要再次破坏兼容。

设计细节见 [实施方案](multi-application-architecture-next.md)，部署操作见 [运维说明](operations.md)。
