# 架构

本文描述当前代码的结构。与代码不一致时以代码为准，并修正本文。

## 概览

RedApp 是单进程 Go 服务：一个二进制、一个 SQLite 数据库、一个数据目录。它为企业内网缓存并分发应用安装器和制品，提供公开目录页与管理后台。前端是嵌入二进制的 Vue SPA。

```text
客户端 ──HTTP──▶ httpserver ──▶ 领域服务（catalog / download / httpcache / hosted / prewarm …）
                     │                 │
                     │                 ├──▶ distributor ──▶ 上游（经三级代理）
                     │                 └──▶ store（SQLite） + 数据目录 objects/
                     └──▶ 嵌入的 SPA（internal/httpserver/web）
```

## 进程与启动

`cmd/redapp` 的子命令：

| 命令 | 作用 |
| --- | --- |
| `serve`（默认） | 启动服务 |
| `version` | 输出版本与提交 |
| `config validate` | 校验部署配置 |
| `healthcheck` | 请求本机 `/health/ready`（容器健康检查用） |

部署配置优先级：命令行参数 > 环境变量 > 配置文件 > 默认值。配置文件来自 `--config`、`REDAPP_CONFIG` 或可选的 `/etc/redapp/config.yaml`。部署配置只含监听地址、数据目录、可信代理和下载并发/大小上限；其余设置都在数据库中，由后台修改。`REDAPP_PUBLIC_URL` 是公共地址设置的环境默认值。

`serve` 的启动顺序：

1. `store.Preflight`：先用 `instance.Check` 确认没有存活的实例持锁，再只读检查已有数据目录，schema 不匹配直接拒绝（见 [SQLite](#sqlite-schema)）。不创建任何文件。
2. 获取 `<data>/instance.lock`（flock），保证同一数据目录只有一个实例；打开数据库。
3. 同步嵌入的预置模板，恢复未完成的应用删除和待删除对象。
4. 构建上游连接池、应用注册表、下载管理器、认证、指标历史、媒体、HTTP 缓存、托管文件、预热与发布维护服务；`httpserver.New` 一次校验全部依赖，并在 store 上安装配置发布协调器（见 [HTTP 层](#http-层)）。
5. 首次启动时生成随机管理员密码并输出到日志。
6. 启动后台循环和 HTTP 服务；收到 SIGINT/SIGTERM 后 15 秒内优雅退出。

`main` 创建唯一的 `log/slog` text logger（标准错误），交给 store、各领域服务、后台循环和 HTTP 层；日志字段和级别规则见 [conventions.md](conventions.md#日志)。

## 包与依赖方向

依赖从上往下，下层不引用上层：

| 层 | 包 | 职责 |
| --- | --- | --- |
| 入口 | `cmd/redapp` | 解析配置、组装依赖、启动服务和后台循环 |
| | `cmd/preset-inventory` | 导出预置清单 JSON，供安装器维护脚本使用 |
| HTTP | `internal/httpserver` | 路由、鉴权与 CSRF、请求解析、错误响应、SPA 与分发路径；嵌入前端产物 |
| 编排 | `internal/apps/builtin` | 把编译期的发布协议与数据库中的应用组合成运行时注册表 |
| | `internal/prewarm` | 单 worker 的有界预热任务 |
| | `internal/releasemaintenance` | 定时保留最新 N 个版本，并触发自动预热 |
| 领域 | `internal/catalog` | 渠道/元数据缓存与 TTL、合并重复请求、制品授权 |
| | `internal/download` | 发布制品的下载引擎：代际、读写限额、续传、校验、清理与保留 |
| | `internal/httpcache` | `http-cache` 应用的可变 HTTP 响应缓存：按路径的条目、边下边读的回源、刷新与清理 |
| | `internal/hosted` | 管理员上传的托管文件 |
| | `internal/history` | 指标采样与按小时 UTC 聚合 |
| | `internal/auth` | 管理员密码、内存会话、CSRF、登录限速 |
| | `internal/site` | 双语站点文本设置 |
| | `internal/config` | 部署配置与公共地址 |
| | `internal/configexchange` | 配置导入导出的文档格式与校验（不可信输入） |
| 协议 | `internal/apps/codex` | Codex 发布元数据协议与版本规则 |
| | `internal/apps/claude` | Claude 清单协议、平台规则与签名验证 |
| | `internal/application` | Provider 定义与能力、应用快照、注册表、发布协议接口 |
| | `internal/distributor` | 有界的上游 HTTP 客户端与按作用域的代理 transport |
| 存储 | `internal/store` | SQLite schema 与全部持久化；底层连接不对外暴露（见 [SQLite](#sqlite-schema)） |
| 基础 | `internal/identity` | ID 校验、保留名、UID 与存储命名空间 |
| | `internal/cachepolicy` | HTTP 缓存规则与自动清理规则的类型和校验 |
| | `internal/warmplan` | 预热计划的上限与字节预算 |
| | `internal/networkproxy` | 代理设置的数据类型与继承解析（不含 transport） |
| | `internal/pathmatch` | 应用内相对路径匹配 |
| | `internal/media` | 图标（SVG 白名单、PNG/JPEG 重编码）按内容哈希存储 |
| | `internal/fsutil` | 持久文件原语：建目录、目录 fsync、拒绝符号链接的只读打开、暂存文件或已写完文件的 rename 发布、原子写、删除、随机 ID |
| | `internal/spool` | 两个缓存引擎共享的流式文件核心：单写者填充、多读者跟随、有界重试与字节范围续传、整文件校验与并发校验合并、文件状态 |
| | `internal/jsoncheck` | 拒绝重复键、过深嵌套和尾随数据 |
| | `internal/yamlconfig` | 严格的单文档 YAML → JSON |
| | `internal/instance` | 数据目录实例锁与只读的持锁检查 |
| | `internal/logging` | 日志约定：`component` 字段、遮盖 URL 凭据的 `error` 字段、未传 logger 时的丢弃默认值 |
| 测试 | `internal/testutil` | 基于 httptest 的上游客户端；在真实 store 中创建目录应用并构造其运行时条目（`App`、`Entry`）；收集结构化日志的 `Logs`（仅测试使用） |
| | `internal/store/storetest` | 测试另开一个到数据目录数据库的连接，用于故障注入和没有 store API 的夹具（仅测试使用） |
| 嵌入数据 | `presets/` | 内置厂商、应用、分类的 YAML 模板与图标 |
| | `installers/` | 嵌入 generated 安装脚本、许可证和公钥（见 [installers.md](installers.md)） |

不符合理想方向、待重构的依赖：`store` 引用 `configexchange` 和根目录的 `presets`；`distributor` 引用 `store`。

## 身份模型

- **厂商（vendor）与应用（app）**：ID 都满足 `^[a-z0-9]+(?:-[a-z0-9]+)*$`，最长 63 字节。应用的完整键是 `<vendor>/<app>`，所有 API 和 UI 都用完整键，不推断默认应用。
- **保留名**：厂商 ID 不能是 `admin`、`api`、`assets`、`health`、`all`（`internal/identity`），因为它们与顶级路由冲突。应用 ID 没有保留名。
- **UID**：每个厂商和应用有 32 位小写十六进制的随机 UID，创建后不变。厂商 ID、应用 ID、所属厂商和 Provider 也不可修改。
- **存储命名空间**：指标用 `app/<uid>`，生命周期内不变；缓存用 `app/<uid>-e<epoch>`。上游地址或回源策略变化时 `source_epoch` 递增，旧缓存自然失效。

公开路由：

| 路径 | 含义 |
| --- | --- |
| `/`、`/all` | 首页与全部应用（SPA） |
| `/<vendor>` | 厂商页（厂商启用且未删除；否则返回同一文档并带 404） |
| `/<vendor>/<app>` | 应用详情与使用说明（SPA，应用未发布时返回同一文档并带 404）；带尾部 `/` 时 308 重定向 |
| `/<vendor>/<app>/<file_path>` | 分发路径：安装脚本、静态资产、渠道/元数据或制品 |
| `/api/...`、`/admin/api/...` | 公开 API 与管理 API |
| `/admin/...` | 后台 SPA；只有规范 `x-spa-routes` 中的路径（且厂商、应用和标签页存在）返回 200，其余 `/admin/...` 返回同一文档并带 404 |

每条路由的请求、响应和错误码定义在 [`api/openapi.yaml`](../../api/openapi.yaml)，说明见 [api.md](api.md)。

## 提供者（Provider）

Provider 在编译期定义（`internal/application/providers.go`，[ADR 0002](adr/0002-compile-time-providers.md)），同一列表也写进 schema 的 CHECK 约束：

| Provider | 分发内容 | 服务方式 |
| --- | --- | --- |
| `info` | 无文件，只有详情和使用说明 | — |
| `hosted` | 管理员上传的文件 | `hosted.Service`，`http.ServeContent`（支持 Range） |
| `http-cache` | 任意上游路径的 HTTP 缓存，1–16 个上游，顺序/轮询/随机 | `httpcache.Service` |
| `codex` | Codex 发布元数据和制品 | 发布协议 + `catalog` + `download` |
| `claude-code` | Claude Code 签名清单和制品 | 发布协议 + `catalog` + `download` |

发布类 Provider（codex、claude-code）实现 `application.Protocol` 接口：解析路径、校验/比较版本、解析渠道、获取并验证发布元数据、渲染。`internal/apps/builtin` 只接受预置中声明的 `codex-releases-v1` 和 `claude-manifest-v1` 协议。

- **Codex**：`release.json` 中每个资产必须有 `sha256:` 摘要，资产 URL 只能指向配置的上游或官方地址，实际下载地址总是由配置的上游重新构造。
- **Claude Code**：`manifest.json` 必须有分离签名 `manifest.json.sig`。服务端用嵌入的固定公钥（指纹 `31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE`）验证 OpenPGP RSA-4096/SHA-512 签名，拒绝额外数据包、未来时间和过期签名。原始清单与签名按原字节保存和返回。客户端只校验摘要，不自行验签。

分发路径的解析顺序：托管文件和 http-cache 文件先被拦截；其余由应用条目的 `ParsePath` 依次匹配公开资产、安装脚本、descriptor 资产，最后交给发布协议。渠道/元数据请求走 `catalog.Represent`，制品请求走 `catalog.Authorize` 获得已授权的 `download.Resource` 后由下载引擎提供。

## 下载引擎（`internal/download`）

服务 codex、claude-code 的发布制品。

- **资源身份**：`sha256(app \0 version \0 key)`，并与持久化的 `resources` 行（来源 URL、SHA-256、期望大小）和应用上游核对，其他应用不能借用缓存身份。
- **代际（generation）**：每次下载是一个代际，状态包括 downloading、resuming、retry_wait、verifying、complete、failed、invalid、interrupted。每个资源最多一个当前代际（部分唯一索引）。
- **读者与写者**：一个写者经 `spool.Fill` 填充 part，多个读者跟随同一个 `spool.Body` 边下载边读取；`Body` 有自己的锁，读者不争用 `Manager.mu`，失败只以错误结束读取，从不表现为成功的 EOF。默认上限 16 个写者、512 个读者，单制品 4 GiB；`httpcache` 和 `hosted` 共用这组额度。
- **续传**：带 `Range: bytes=N-`，强 ETag 时加 `If-Range`。只接受精确的 206、`Content-Range` 和相同 ETag（`spool.CheckResume`）；其他情况放弃续传，新建一个完整重下的代际。每 1 MiB 记录进度。
- **写入顺序**：进度在 `Manager.mu` 内取快照并分配该代际的下一个 `checkpoint` 序号，释放锁后写库；`SaveGeneration`、`CompleteGeneration` 只在序号比库中新时生效，迟到的旧快照不会覆盖新状态或撤销完成。`mu` 只在改变“当前代际”的写入（创建、退役、完成、删除）和 blob 发布时持有，保证内存与 `generations` 表一致。
- **超时与重试**：由 `spool.Fill` 执行。下载流没有总时限，只有空闲读超时（单次读取 60 秒无数据即中断）；连接、TLS、响应头各有独立时限，元数据读取限时 5 分钟。连接错误、读取中断或截断、5xx、408、429 会重试，最多 6 次，退避从 1 秒翻倍、上限 30 秒并加随机抖动；其他 4xx、磁盘、编码和完整性错误不重试。重试耗尽时，如果已有数据且上游支持续传（ETag 或字节范围），保留 part 并标记 interrupted，下次请求从断点续传。
- **错误分类**：上游错误为类型化的 `distributor.RequestError`（DNS、TLS、超时、重定向、网络）；本地失败（哈希、长度、磁盘、数据库、续传）是带固定消息和事件类别的类型化错误。失败类别只按错误类型判断，不匹配错误文本。
- **校验与发布**：写入 `objects/parts/<gen>.part`，完成后校验完整 SHA-256 和大小，fsync 后 rename 到 `objects/blobs/<sha256(app)>/<hash>.blob`，fsync 目录，再在数据库标记完成。校验失败的代际为 invalid。
- **校验不持锁**：整文件哈希不在 `Manager.mu` 内进行。同一资源的并发请求共享一次校验，校验由管理器自己的 goroutine 和 context 执行，请求方取消只是停止等待，不会让有效缓存被判为无效。校验结束后重新加锁，确认代际仍是当前代际、文件 inode/大小/修改时间未变，才应用结果；校验中的代际视为活跃，不会被清理或清除。
- **恢复**：启动时不对已提交的完整 blob 做整文件哈希，只核对存在和大小，并标为待校验；首次被请求时复用惰性校验（与源失效后恢复的 dormant 代际相同），校验失败才退役重下。已改名为 blob 但尚未提交的代际在恢复时立即校验。清理孤立的 part 和 blob。
- **清理与保留**：版本清理和保留都是[冻结预览](#冻结预览)，执行只作用于预览中的代际；保留策略按应用保留最新 N 个版本，有效渠道、正在读写的代际和无法比较的版本受保护。
- 下游响应目前由服务端自行流式输出，不支持客户端 Range。

## HTTP 缓存引擎（`internal/httpcache`）

服务 `http-cache` 应用：按 `(storage_id, path)` 缓存上游的可变 HTTP 响应。与下载引擎共享 `internal/spool` 的流式核心和 `internal/fsutil` 的发布原语；按路径的条目生命周期（当前/退役、读者 pin）属于 HTTP 缓存自己，SQL 全部在 `internal/store`（`http_cache.go`）；刷新与清理是[冻结预览](#冻结预览)。

- **回源与合并**：同一路径、同一应用快照（运行时 revision）和同一被替换条目的并发请求合并为一个 flight。来源按策略顺序尝试，只在来源响应前失败（连接错误、超时、5xx）时换下一个来源；全部失败且允许时回退到旧条目。回源带 `If-None-Match`/`If-Modified-Since`，`304` 只更新验证时间和响应头。
- **流**：可缓存的 `200` 响应成为一个流：一个 `spool.Fill` 写入 `objects/http/<id>.part`，flight 的等待者和之后加入的请求都作为读者跟随同一个 `spool.Body` 边下边读。没有总时限，只有单次读取 60 秒空闲超时。读取失败后只向同一来源续传：`Range` + `If-Range`（非弱 ETag，或比 `Date` 至少早 1 秒的 `Last-Modified`），续传响应必须是同一表示（验证器相同）的精确剩余区间且仍可缓存，否则流失败；没有强验证器的流不续传。重试上限与退避与下载引擎相同。最后一个读者离开时停止填充，该流在同一次持锁中不再接受新读者，之后的请求重新回源；加入后才发现流已被停止的请求同样重新回源。
- **发布**：写入时计算 SHA-256；完整且长度一致后读者即可读到 EOF，随后 fsync、rename 为 `<id>.body`，再在 store 的一个事务中检查来源 fence（替换时还确认旧条目仍为当前）并发布为当前条目。发布与停止接受新读者在同一次持锁中完成，之后清理退役该条目时不会再有读者加入这个流。失败、超限、被放弃或发布被拒绝（来源已变化、旧条目已被清理）的流删除 part，不留条目；已经开始接收的客户端连接被中断。读完整个文件的请求等到发布结束才返回，下一个请求一定能看到条目。
- **服务**：已存储的条目由 `http.ServeContent` 处理条件请求和单段 Range，读取期间 pin 住条目，退役后最后一个持有者释放时删除文件和行。流上的请求先等待首字节，立即失败的流在发送任何内容之前报错；来源声明了长度时同样经 `http.ServeContent` 处理并等待所需字节，否则返回完整的 `200`。流的响应只带上游 ETag；存储后没有上游 ETag 时生成 `"sha256-<hex>"`。
- **新鲜度**：上游 `s-maxage`/`max-age` 减去 `Age` 优先；只有完全没有 `Cache-Control` 时才用应用默认 TTL。路径规则（最多 32 条，首个匹配生效）可覆盖 TTL 和 `no-store`/`private`，但不缓存带 `Set-Cookie` 或不支持的 `Vary` 的响应。请求端缓存指令被忽略，只支持 `only-if-cached`。不可缓存的响应由每个读者各自直接传输，受写者额度约束。
- **恢复**：启动时只删除 part、没有条目的 body 和已退役条目，不计算哈希。每个条目在本进程首次使用前校验一次：`spool.Checks` 合并同一条目的并发校验，校验在服务自己的 context 中运行，等待者取消不会中断它；不一致或缺失的条目被退役并重新回源。本进程写入的条目不再校验。
- **额度与指标**：读者和写者占用 `download.Budget` 的共享额度，流在整个填充期间持有一个写者额度。`Files()` 以 `spool.FileStatus` 报告条目和正在填充的 part，与下载引擎的 `Files()` 一起用于容量和磁盘指标。
- 这里的哈希只用于存储完整性，不用于授权。

## 冻结预览

版本清理、保留、HTTP 缓存刷新和 HTTP 缓存清理共用一套“预览 → 审阅 → 只执行冻结集合”的机制（`internal/store/previews.go`，表 `previews` 与 `preview_items`）。各类型只提供选择和执行逻辑。

| 共享部分 | 规则 |
| --- | --- |
| 记录 | 类型、应用 UID、来源 epoch、来源 fence（应用与厂商的运行时 revision）、是否要求来源在服务、创建与过期时间、状态、计数（扫描、选中、字节、使用中、完成、失败）、类型自有的条件（`criteria_json`）与估算（`summary_json`）、回执 |
| 条目 | 冻结的候选按序号（正整数，显示顺序）存储：对象引用、显示标签、大小、是否选中、类型自有的细节、结果状态与错误码；未选中的条目只供审阅，状态为 `kept` |
| 状态 | `building`（分页冻结中）→ `ready` → `running` → `done`/`failed`；保留与版本清理在一个事务内从 `ready` 直接到 `done` |
| 过期 | 未执行的预览创建后 10 分钟过期；回执在执行后保留 24 小时；执行中的不过期。过期是由时间推出的，`PrunePreviews` 分批删除（每批最多 1000 个条目） |
| fence | 创建、冻结每一页、开始执行和执行每一批都在同一事务中核对来源 fence 与类型守卫（保留：策略哈希、渠道与当前 epoch）；不符为 `ErrPreviewStale`，什么都不改 |
| 执行 | `ClaimPreview` 把 `ready` 原子地改为 `running`，并发的执行只有一个成功，其他得到 `ErrPreviewRunning`；已有回执的预览再次执行时返回同一回执。条目只记录第一次结果。重启时把遗留的 `building`/`running` 标为 `failed`，不续跑 |
| 分页 | 条目按序号游标分页，执行时位置不变；保留的版本序号从 1 连续，因此页码分页也落在固定序号上 |
| 删除 | 预览以应用 UID 外键级联，永久删除应用时一并删除 |

| 类型 | 选择 | 执行 | 要求来源在服务 |
| --- | --- | --- | --- |
| `version_cleanup` | 低于最低版本的当前代际，按版本成条目 | 一个事务内退役仍为当前的冻结代际；正在读写的在传输结束后删除 | 否 |
| `retention` | 全部评估过的版本，超出最新 N 个的被选中 | 一个事务内整版本退役；有代际已变化或正在读写的版本跳过，回执记录原因 | 是 |
| `cache_refresh` | 匹配模式的当前条目，按缓存行号分页冻结到高水位 | 后台 worker 逐条重新验证并记录结果 | 是 |
| `cache_cleanup` | 匹配模式且早于截止时间的当前条目，同上 | 每批最多 100 条，跳过已被替换或在预览后被访问的条目 | 自动清理是，手动清理否 |

HTTP 层只有一处映射（`previews.go`）：未知、其他应用或其他类型、过期的预览为 `PREVIEW_NOT_FOUND`；fence 或守卫变化为 `PREVIEW_STALE`（创建时为 `SOURCE_CHANGED`）；构建或执行中为 `OPERATION_IN_PROGRESS`。`ENTITY_DELETED` 与 `APPLICATION_DISABLED` 由处理函数在构建或执行前按各操作的规范检查。

## 磁盘布局

```text
<data>/
  instance.lock          # flock 实例锁，0600
  state.sqlite(-wal,-shm)  # SQLite，WAL 模式
  objects/
    parts/               # 下载中的发布制品
    blobs/<sha256(app)>/ # 已校验的发布制品
    http/                # HTTP 缓存响应体 <id>.body，填充中的 <id>.part
    hosted/              # 托管文件
    icons/               # 上传的图标
```

目录权限 `0700`，文件 `0600`。`/health/ready` 会在数据目录写入并删除一个临时文件来检查可写性。实例锁依赖 `flock`，只支持 Unix。

## SQLite schema

- schema 内嵌在 `internal/store/schema.sql`，版本写入 `PRAGMA user_version`，常量为 `store.SchemaVersion`（当前为 15）。`PRAGMA application_id` 固定为 RedApp 的标识，用来拒绝版本号碰巧相同的其他 SQLite 文件。
- 新目录（为空或只含实例锁）创建全新 schema，并在首次启动前 checkpoint 到主文件。已有数据库以只读、immutable 方式检查 `application_id` 与 `user_version`，任一不符就拒绝启动，不改写、不删除，也不创建 WAL/SHM 文件。不比较表结构：1.0 前每次 schema 变化都提升版本。
- 迁移框架（`migrate.go`）在 1.0 前处于休眠状态：`MinimumMigratableVersion` 为 0，任何其他版本都被拒绝。启用后先用 `VACUUM INTO` 在数据目录写备份，再在一个事务中执行迁移步骤、外键检查并与全新 schema 比对；流程和发布 1.0 的步骤见 [development.md](development.md#schema-迁移)。每个 schema 版本的 golden fixture 在 `internal/store/testdata/schema/`。
- 属于厂商或应用的行以 UID 引用父行并 `ON DELETE CASCADE`；应用引用厂商不级联，因为必须先删除应用并登记其对象文件。发布、缓存和指标数据以存储命名空间或指标命名空间为键，永久删除应用时按前缀删除；同一版本的元数据、渠道、资源和下载代际随版本级联删除。
- 1.0 前不运行迁移，规则见 [ADR 0001](adr/0001-pre-1.0-no-migrations.md)。
- 连接：WAL、`synchronous=FULL`、外键开启。写连接只有一个（`_txlock=immediate`），所有写入和读改写事务都在它上面串行；只读查询和只读快照事务走独立的 `query_only` 读连接池（8 个），WAL 下不等待正在进行的写事务，看到的是最近一次提交。持有写事务时（包括 `finalize`、`beforeCommit` 和删除包装回调）只能使用该事务，不能调用会写的 `Store` 方法，否则会等待调用者自己占用的写连接；只读方法可以调用，但看不到事务内未提交的修改。
- 其他包不能拿到底层连接：读写都通过 `Store` 的类型化方法，找不到行时返回 `store.ErrNotFound`。跨包测试用 `storetest.Open` 另开连接做故障注入。
- 流量与请求计数先在内存累加，每秒、每次传输结束、每次读取计数前以及关闭时批量写入一个事务；写入失败保留增量重试，不影响传输。异常退出最多丢失约 1 秒的计数。

## 配置模型

### 模板与稀疏覆盖

- 内置厂商、应用和分类定义在 `presets/` 的 YAML 中（`<vendor>.yaml`、`<vendor>/<app>.yaml`、`_taxonomy.yaml`），嵌入二进制。启动时同步为模板快照。
- 每个厂商/应用的配置二选一：**引用模板 + 稀疏覆盖**，或**独立 spec**。有效值 = 模板 spec 合并覆盖；`proxy`、`prewarm`、`retention` 整体替换，其他对象深度合并。
- 修改用 `{revision, set, unset}`：`set` 写入覆盖，`unset` 删除覆盖、恢复模板值。JSON `null` 一律拒绝。
- 可信的分发声明（`distribution`：安装器、资产、协议）只来自嵌入的预置，单独保存；导入的配置不能携带。

### 三级代理继承

代理按 应用 → 厂商 → 全局 解析：

| 模式 | 含义 |
| --- | --- |
| `inherit` | 使用上一级设置（全局不能设为 inherit） |
| `direct` | 直连，截断继承 |
| `url` | 使用完整的代理 URL（http、https、socks5，可带凭据） |

全局未设置时为直连。`url` 模式下 DNS 由代理解析。每个作用域有独立的 transport，切换设置不取消已在进行的请求。

### 写入、CAS 与发布

配置写入（`internal/store` 的 `writeConfiguration`）分四步：

1. **读取**：在只读事务中只加载本次操作涉及的行——目标实体、它的模板、所属厂商（解析代理）、引用的分类、备注、全局代理——在内存中修改，再校验并物化变化的实体（目录列、使用说明 revision、HTTP 策略、来源 epoch、运行时 revision）。
2. **准备**：在内存中的运行时视图上叠加变化的实体，得到完整的 `DirectorySnapshot`，交给发布协调器准备注册表、代理 transport 与下载上游。此时没有打开的事务。
3. **提交**：一个事务只写变化的行。已有的厂商、应用、分类、备注和全局代理用 `UPDATE … WHERE … AND revision=<读到的值>`，新建的行用 `INSERT … ON CONFLICT DO NOTHING`；影响行数不为 1 即 `ErrConflict`，整个事务回滚。分类的创建、剪枝和公开 revision 在同一事务内完成；永久删除在同一事务内清除行。
4. **发布**：提交成功后才发布，并把变化并入运行时视图。准备或提交失败都会放弃发布，视图不变。

要点：

- **按实体冲突**：只有本次写入涉及的实体被并发修改才冲突；修改其他实体从不冲突。导入在执行时对涉及的实体重新计算计划，与预览结果逐项比较（revision、差异、动作），任一不同即 409；全部实体、分类、备注和导入回执在一个事务内提交或全部不提交。
- **三级代理**：修改厂商或全局代理只写自身那一行，不改写应用的覆盖；应用的有效视图和运行时代理作用域在读取或发布时由三级继承解析。
- **运行时视图**：store 在内存中保存已提交配置的运行时投影（实体、自身代理设置、模板绑定、可信发布契约、来源），首次发布时从数据库加载一次。准备发布不再重读配置，但重建注册表仍是内存中 O(N)。`DirectoryConfigurationSnapshot` 与 `RepublishConfiguration` 从数据库重建视图，用于启动和外部直接改行之后。
- **唯一的全局串行点**：`Store.writeMu` 从读取持有到发布，保证发布顺序与提交顺序一致、每次准备都基于上一次提交后的视图。持锁的最长操作是一个只涉及变更行的写事务加内存中的准备与发布；SQLite 本来就只允许一个写者。不参与发布的管理员备注只用 CAS，不持此锁；分类重命名持此锁，以免与写入中新建的分类重名。HTTP 层不再有自己的目录锁。
- 模板同步（启动时）是唯一加载全部配置的写入，因为模板变化会影响所有绑定它的实体。

### 其他设置

- **保留**：发布类应用可设 `keep_latest`（1–1000，默认 3）。
- **预热**：按渠道和平台白名单选择要预先下载的制品。
- **HTTP 缓存策略**：`http-cache` 应用的路径规则与自动清理规则（`internal/cachepolicy`）。
- **导入导出**：ZIP 或单个 YAML，格式与 `presets/` 一致；有大小与数量上限，先预览再执行，失败整体回滚。

## 后台循环

| 循环 | 周期 | 位置 |
| --- | --- | --- |
| 计数落盘 | 1 秒 | `store.StartCounterFlush` |
| 指标采样 | 1 分钟 | `httpserver.SampleHistory` |
| 发布保留 + 自动预热 | 15 分钟 | `releasemaintenance.Run` → `prewarm.Automatic` |
| HTTP 缓存自动清理 | 15 分钟 | `httpcache.RunCleanup` |

另有按需启动的 goroutine：每个下载代际、元数据获取、HTTP 缓存回源、流填充、条目校验与刷新、单个预热任务、托管文件传输。过期会话和登录记录没有清理循环，在访问时惰性清除。

## 安全边界

摘要如下，完整说明见 [docs/guide/security.md](../guide/security.md)。

- **上游信任**：只从配置的固定上游获取；Codex 依赖 HTTPS 和官方元数据中的摘要，Claude 额外验证固定公钥签名。下载完成并校验摘要后才发布缓存。回源强制 `Accept-Encoding: identity`，拒绝非 identity 编码（[ADR 0003](adr/0003-no-http-compression.md)）。
- **重定向地址类别**：内置公共来源只连接公网地址。管理员配置的来源主机可以在内网，但重定向到其他主机时只能连接与来源同类的地址（公网来源只到公网，非公网来源只到非公网；来源同时解析到两类时拒绝跨主机重定向）。检查在拨号时按解析出的 IP 进行，并直接连接这些 IP，DNS 重绑定无法绕过。配置出口代理时由代理解析主机名，本地不做此检查。
- **管理认证**：单个管理员密码（bcrypt），内存会话（8 小时，`Path=/admin`、HttpOnly、SameSite=Strict），所有非 GET 请求要求 `X-CSRF-Token`，并检查 Origin。登录按 IP 限速。
- **输入校验**：JSON 拒绝重复键和未知字段，有大小上限；查询参数白名单；SVG 按白名单解析，位图重编码。
- **响应头**：全部响应带 `nosniff`、`X-Frame-Options: DENY`、`Cache-Control: no-store`；SPA 有严格 CSP。
- **使用说明文档**：管理员编写的 HTML/JS，将在无同源权限的沙箱 iframe 中运行（[ADR 0006](adr/0006-sandboxed-usage-instructions.md)）。

## HTTP 层

`internal/httpserver` 按 [`api/openapi.yaml`](../../api/openapi.yaml) 实现全部路由，设计规则见 [api.md](api.md)。

### 构造

- `httpserver.New(Deps, ...Option)` 一次校验全部依赖（store、注册表、目录、下载、HTTP 缓存、托管文件、认证、上游连接池、图标、指标历史、公共地址、预热、发布维护、数据目录），缺失即返回错误；不做惰性初始化。
- `Deps.TrustedProxies`（`ParseTrustedProxies`，来自部署配置 `trusted_proxies`）决定哪些对端的转发头可信。
- 构造时在 store 上安装配置发布协调器（`publication.go`）：每次配置写入先准备候选注册表、上游 transport 和下载上游，提交后一起发布（见[写入、CAS 与发布](#写入cas-与发布)）。发布顺序由 store 保证，处理器不另加锁。
- 选项：`WithConfigurationCheck` 在发布准备后追加一个校验（测试用它注入失败），`WithDeleteWait` 设定删除应用时等待任务退出的上限（默认 15 秒）。

### 路由表

`routes.go` 的 `routeTable()` 每个规范操作一行（共 103 个），按领域分段，可以直接与规范对照：

| 字段 | 含义 |
| --- | --- |
| `method`、`path`、`operation` | 与规范完全一致（路径模板、operationId） |
| `serve` | 处理函数 |
| `auth` | `authAdmin`：会话 cookie，非安全方法另需 `X-CSRF-Token` |
| `query` | 查询参数白名单，未知、重复或空值返回 `400 INVALID_QUERY` |
| `maxBody` | `x-max-body-bytes` |
| `handlerChecksQuery` | 由处理函数按资源决定查询串的错误码（分发路径） |
| `servedBy` | 由另一个操作的注册一并处理：HEAD 由 GET 模式应答，安装脚本由文件路由分派 |
| `paths` | 改为逐条注册这些路径；用于 `/admin/{ui_path}`（按 `x-spa-routes` 注册，见 `spa.go` 的 `adminSPARoutes`） |

`muxPattern` 把路径模板转换成 ServeMux 模式：`x-greedy` 参数变为 `{name...}`，`/` 变为 `/{$}`。`/{vendor}`、`/{vendor}/{app}` 和文件路由遇到保留厂商名时由 `reservedPath` 处理：`/admin/...` 页面返回后台文档并带 404；`/admin/api/`、`/api/`、`/assets/`、`/health/` 下没有操作的路径由 `reservedRouteError` 应答，路径有其他方法的操作时为 `405`（`Allow`），否则为 `404 NOT_FOUND`。文件路由的 GET 模式匹配任意 GET 路径，所以这些路径不能依赖 ServeMux 自己的 404/405。

新增一个路由：先改规范；在 `routeTable()` 对应分段加一行（查询参数、上限、鉴权与规范一致，`TestRouteTableMatchesSpec` 会检查）；写处理函数，只用显式错误码和显式响应类型；用 `newHarness` 写行为测试，响应会被自动按规范校验。

### 中间件

`ServeHTTP` 对所有请求依次执行：

1. 生成 `request_id`（16 位十六进制），写入 `X-Request-Id`、请求上下文和日志；结束时写一行访问日志（不含查询串）。
2. panic 恢复：未写响应头时返回 `500 INTERNAL_ERROR`，已开始流式输出则中断连接；`http.ErrAbortHandler` 原样上抛。
3. 默认安全响应头（`nosniff`、`Referrer-Policy`、`X-Frame-Options: DENY`、`Cache-Control: no-store`）。
4. 请求 origin（TLS、`Host`、可信代理的转发头），无效返回 `400 REQUEST_ORIGIN_INVALID`。
5. 规范路径检查（`.`/`..`、`//`、反斜杠、NUL、`%2F`/`%5C`），否则 `400 INVALID_PATH`。
6. ServeMux 匹配；未匹配时把 ServeMux 的 404/405（含 `Allow`）改写成 `Error` 文档（保留路径见上文 `reservedRouteError`）。

每条路由再依次执行：`/admin/api/` 的 Origin 检查（`403 ORIGIN_REJECTED`）、会话（`401 AUTH_REQUIRED`）、CSRF（`403 CSRF_REJECTED`）、查询白名单、请求体上限，然后进入处理函数。

### 请求与响应辅助

| 文件 | 内容 |
| --- | --- |
| `request.go` | `checkQuery`、`queryInt`、`pageQuery`、`queryText`；`decodeJSON`（`application/json`、415、413、`jsoncheck.Strict`：重复键、无效 UTF-8、`null`、过深嵌套，再拒绝未知字段和尾随数据）、`decodeJSONNullable`；`ifMatch`（只接受一个 `"<正整数>"`，否则 `400 IF_MATCH_REQUIRED`）、`revisionConflict` |
| `response.go` | `writeOK`、`writeRevision`（带 `ETag`）、`writeCreated`（201、`Location`、`ETag`）、`writeNoContent`；`optionalText`、`utcTime` |
| `page_cursor.go` | 游标分页的唯一实现：`cursorPage[T]`（`items`、`next_cursor`）、`pageCursor`（绑定 operationId 和范围摘要，范围内的 UID 等内部标识只以摘要出现）、`decodeAfterCursor`/`afterCursor`、`invalidCursor`；页码分页用 `pageQuery` 与 `pageDTO[T]` |
| `errors.go` | `newError(code, cause, message)`、`writeError`、`s.fail(w, r, code, cause, message)`、`storageError`；`cause` 只进日志（URL 凭据被遮盖），`message` 是面向用户的英文 |
| `error_codes.go` | 规范 `components.x-error-codes` 的 Go 常量与状态/`retryable` 表；`TestErrorCatalogMatchesSpec` 保证两者一致 |
| `public_dto.go` 等 | 响应文档类型，按规范 schema 显式构造；不直接编码 store 或领域结构体 |

公开文件下载（发布制品、HTTP 缓存与托管文件）经 `download_ranking.go` 的 `downloadReceipt` 写出：每次写入前把写截止时间推后 60 秒，替代服务器 10 分钟的 `WriteTimeout`，因此只有客户端停止接收时才超时；应用工作取消设置的中止截止时间不会被推后。

领域错误到错误码的映射集中在使用它的处理文件中（如 `distribution.go` 的 `releaseError`、`cacheFileError`，`directory.go` 的 `directoryFailure`），按 sentinel 或类型判断，不看错误文本。store 的校验错误是 `store.ValidationError`（匹配 `ErrInvalidDirectory`），其 `Detail()` 指明出错字段、不含已保存的机密，可作为 `VALIDATION_FAILED` 的消息；导入预览和执行仍用通用消息，因为细节可能回显包中的私有 URL 或文本。

### SPA

`spa.go` 定义构建布局：公开页面用 `index.html`，后台页面（含其 404 文档）用 `admin.html`，静态资源在 `assets/`，都位于构建目录根部。后台页面从不回退到公开入口，缺少入口文件时返回 `500 INTERNAL_ERROR`。前端产物来自 `Deps.Frontend`（默认是嵌入的 `internal/httpserver/web`；测试用固定的小型产物 `frontendFixture`），构建布局变化时只改这几个常量。未发布的厂商和应用页面返回 `index.html` 并带 404。

### 测试

- `harness_test.go` 的 `newHarness(t, options...)` 是唯一的 HTTP 测试工厂：真实 store 与全部服务、临时数据目录、`New` 构造的服务器、httptest 监听和带 cookie 的客户端。选项有 `withDir`（重启同一目录）、`withOptions`、`withTrustedProxies`、`withEmbeddedFrontend`。
- 辅助方法：`login`、`request`/`raw`/`expectError`、`serve`（进程内请求，可指定 `RemoteAddr`/`Host`）、`createVendor`/`createApp`（直接写 store，不依赖被测管理 API）、`publicCatalog`、`upstreamProxy`+`releaseApp`+`codexRelease`（发布类应用经全局代理连到测试上游）、`logs`（服务器结构化日志）。
- 夹具：`store_fixtures_test.go` 经 store 修改配置（`patchApp`、`setAppEnabled`、`setVendorEnabled`、`markDeleted`），供目录以外领域的测试使用；`directory_helpers_test.go` 经管理 API 操作目录与配置，供目录领域的测试使用。
- 契约校验：客户端 transport 和 `serve` 对每个响应找到规范操作（ServeMux 优先级，HEAD 回落到 GET），检查状态已声明、`X-Request-Id`、安全头、已声明的响应头、媒体类型、JSON 响应体（JSON Schema 2020-12，`santhosh-tekuri/jsonschema`）；错误响应还检查错误码属于该操作的错误码集合（`x-error-codes` 加适用的组）、状态与 `retryable` 与目录一致、`request_id` 与响应头一致。校验器实现在 `openapi_test.go`，`TestContractValidatorRejectsNonConformingResponses` 确认它确实会拒绝不符合的响应。
- `spec_test.go`：路由表与规范一致、`x-spa-routes` 与 `adminSPARoutes` 一致、错误码目录一致，以及规范自身的结构检查（`$ref` 可解析、operationId 唯一且为 camelCase、标签已定义、路径参数一致、错误码在目录中且状态已声明、属性名为 snake_case、全部 schema 可编译）。

## 已知问题与重构方向

| 问题 | 现状 | 方向 |
| --- | --- | --- |
| 锁内数据库调用 | 下载 `Manager.mu` 覆盖改变当前代际的单行写入和 blob 发布，配置写入的 `Store.writeMu` 覆盖一次写事务与发布，两者都在锁声明处说明了原因和持锁范围。HTTP 缓存的条目查询、pin、发布与回收仍在 `Service.mu` 内调用 store，以保持条目行、pin 计数与回收一致（来源检查和访问记录已在锁外） | HTTP 缓存：锁外读取条目，加锁后复核仍为当前再 pin；回收同理 |
