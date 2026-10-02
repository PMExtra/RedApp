# 多应用分发与前端重构实施设计

状态：核心本地实现及 CLI/DOM 集成验证已完成；独立指标前端补丁整合受阻，未发布。日期：2026-10-02。实际结果与未完成项见[本地验证记录](multi-application-validation.md)。

代码证据基线：v0.5.0 / `88c789e023597774a1ca1b812887b46b59a3f49c`。本设计覆盖后续用户决定：1.0 前允许接口破坏性变更，不为旧 URL、API、环境变量、Codex 默认值或 storage alias 保留兼容层；这不授权删除真实部署数据。

## 1. 已定案范围

- 应用的规范身份是 `vendor/app`，首批为 `openai/codex`、`anthropic/claude-code`。
- 公共详情是 `/<vendor>/<app>`；分发是 `/<vendor>/<app>/<*file_path>`。不采用 `/apps/` 或 `/dist/` 前缀，不再支持旧分发路径。
- 服务保持单进程、单实例、本地 SQLite 与本地文件缓存。无多租户、动态插件、分布式调度、任意 URL 转发。
- 编译期注册表描述应用，协议适配器理解上游格式和信任规则；共享内核承担 metadata 缓存、资源状态、下载、校验、恢复和清理。
- 身份在 URL、API、配置、DB、事件、指标、前端、installer 维护中保持一致。不存在“未指定应用就是 Codex”。
- 保留现有签名原文字节、固定上游与客户端最终摘要校验边界。共享 blob 不构成资源授权。
- 完整 Vue Router、Public/Admin layout、后台子路由、按页面订阅数据、独立 draft/error、统一初始化和弹层基础样式。
- 首阶段修复已复现的三个问题：TTL 失败切换串用旧值、`versions.total` 漏 Claude、设置页轮询清掉操作错误。
- 首页副句及其英文对应文案的删除由既有开发任务负责。重构后的首页不重新引入该句，不覆盖其他任务的本地 patch。
- 指标精简与图表 hover 由独立任务实施，精简已二次确认：16 项常用、25 项诊断，共 41 项 active；停止 reuse_requests 独立计数写入/历史/展示及 events.recent_total 历史/展示，保留事件详情。旧样本按原周期自然过期，不主动清除；新实例配置、缓存、历史均不导入，使用全新目录；旧目录保留不自动删除。
- PUBLIC_URL 已另行定案为全局设置：后台持久化覆盖 > `REDAPP_PUBLIC_URL` > 安全请求 origin 推导；后台清空取消覆盖。该环境变量是明确需求，不属于为了兼容而保留的旧配置层。
- locale 已另行定案为客户端同步初始化：手动保存值 > `navigator.languages` 支持项 > 列表不可用时的 `navigator.language` > en；不增加 Accept-Language 服务端协商。
- 首版不做跨应用物理去重；保留 logical resource/blob 概念分离及应用内已验证 blob 复用。父任务已按此默认定案，不再送用户选择。

## 2. 已确认的数据边界与工程默认

用户已最终确认配置、缓存和历史均不迁移。新架构使用全新数据目录；旧目录保留，不自动修改或删除。不开发旧版迁移、导入或兼容工具。启动只接受本架构建立的 schema 3 数据目录；发现旧 schema 或非空未知目录，在任何初始化写入之前拒绝并提示配置新目录。

首版只做应用内完整 blob 复用；编译注册的应用全部可用；生产上游及信任规则随受审查清单固定。以上均已定案，不再等待 A/B 决策。

## 3. 模块与接口

### 3.1 包职责

| 包/目录 | 职责 | 不承担的职责 |
| --- | --- | --- |
| `internal/application` | `Key`、Descriptor、版本/资源/分发请求 DTO；唯一的 ID 解析与验证 | HTTP 客户端、DB、运行时插件 |
| `internal/apps/builtin` | 嵌入受审查的 manifest；协议工厂静态连接；构建 Registry；重复身份/未知协议/保留名检查 | 每个请求动态扫描目录或执行脚本 |
| `internal/apps/codex`、`internal/apps/claude` | 原生路径解析、渠道解析、版本比较、manifest 验证、资源授权、metadata 呈现 | 下载状态和通用 DB CRUD |
| `internal/catalog` | 每应用 metadata/channel cache、singleflight、TTL、可信版本与资源原子入库 | 替适配器猜版本协议或跳过签名 |
| `internal/distributor` | 每应用固定上游 Client；可共用代理 transport，验证 origin/path/redirect | 任意用户 URL |
| `internal/download` | 逻辑资源头、generation、共享 reader、校验、blob 发布、续传、全局限额、清理/GC | 理解 Codex/Claude 版本字符串 |
| `internal/store` | schema 3、类型化方法、短事务、旧目录只读识别；复合键约束 | `Put(kind,id,any)` 作为业务通用接口 |
| 现有 `internal/history` | 固定定义表、global/app scope、计数与历史；事件继续由 Store 保存 | 另建指标框架，或让 version/resource/error 字符串生成时序维度 |
| `internal/httpserver` | 显式 UI/API/分发路由；认证/CSRF；错误映射与传输 | 两个 Catalog 专属字段或 `if Claude else Codex` |
| `internal/config` | 启动文件校验；部署配置、可编辑全局/应用设置的类型与来源 | 多份设置的隐式覆盖 |
| `installers/<vendor>/<app>` | 原文、provenance、patch、generated、许可/公钥；共同 embed 接口 | 改写 upstream 或自动轮换信任根 |

`main` 只负责构造 Store、Registry、共享代理、Catalog、Download Manager、Auth、History、Server 和停机顺序。HTTP Server 持有具体的 Registry、catalog.Service、download.Manager，不持有具体 `Codex`/`Claude` 字段。上表表示职责，不要求每行新增一个包；复用现有包与 Store 的类型化方法，不为每张表建立 repository interface、工厂和 mock。

### 3.2 核心类型与依赖方向

以下为接口草案，命名可在实现时按现有 Go 风格调整，但字段语义不可退回字符串默认分支。

```go
type Key struct { Vendor string; App string } // 仅 ParseKey / registry 构造
func (k Key) String() string                 // vendor/app，唯一规范形式

type LogicalResourceKey struct {
    Application Key
    Version     string // 已由对应 adapter 校验为规范版本
    Resource    string // adapter 定义的版本内唯一键，绝不直接作为磁盘路径
}

type Envelope struct { Raw, Signature []byte } // 精确保留原始字节
type Release struct {
    Version string
    Envelope Envelope
    Artifacts []VerifiedArtifact
}
type VerifiedArtifact struct {
    Key string
    Source string // 已按固定 upstream 校验/构造
    SHA256 string
    Size *int64
}
type Representation struct { ContentType string; Body []byte }

type Protocol interface {
    ParseDistributionPath(relativePath string) (Operation, error)
    ValidateVersion(input string) (string, error)
    CompareVersions(a, b string) (Ordering, error) // 含 Unordered；清理保守保留
    ResolveChannel(context.Context, FixedOriginReader, string) (string, error)
    FetchRelease(context.Context, FixedOriginReader, string) (Envelope, error)
    VerifyRelease(version string, envelope Envelope) (Release, error)
    RenderMetadata(release Release, kind MetadataKind, publicBase string) (Representation, error)
}

// Catalog 和 Download Manager 只有一个实现，保持具体类型和普通方法。
// catalog.Service: Release(ctx, app, target), Authorize(ctx, logicalKey)
// download.Manager: Acquire(ctx, resource), PreviewCleanup(app, candidates),
//                   ExecuteCleanup(app, previewID)
```

`Operation` 是封闭的操作集合：installer、license/key、channel、release metadata、artifact。它不是任意 handler 或任意上游 URL。`AuthorizedResource` 由 catalog.Service 产生；Download Manager 仍二次检查 app、source、摘要及持久化绑定，不能只信任调用方传入 labels。

版本、metadata 呈现等协议细节允许不同；例如 Codex 的发布 JSON 需要把下载 URL 呈现为本服务规范分发根，Claude 则原样返回签名原文/签名。安装命令与更新政策取 descriptor，不进入通用 HTTP 分支。

首版唯一的业务实现接口是 Protocol：两个现有上游已经需要不同路径、版本比较、渠道响应和签名/呈现规则。其他类型是简单数据与具体服务，不建立通用任务状态机、服务容器、hook 总线或插件生命周期。Protocol 方法可在实现时合并现有模块里不可分割的获取/校验步骤，不为满足接口拆散信任逻辑。

### 3.3 单一受审查的描述清单

建议 `internal/apps/builtin/manifest.json` 为身份/展示/上游/installer 的权威清单，使用 `go:embed` 编译入服务，Python 维护脚本读取同一份仓库文件。协议工厂用 Go 显式 import 和函数映射静态连接；未知 protocol 在启动/CI 失败，不加载动态代码。

示例字段：

```json
{
  "schema_version": 1,
  "applications": [
    {
      "id": "openai/codex",
      "name": {"en": "Codex CLI", "zh-CN": "Codex CLI"},
      "publisher": "OpenAI",
      "summary": {"en": "Coding agent for the terminal.", "zh-CN": "终端编程助手。"},
      "protocol": "codex-releases-v1",
      "trust_revision": 1,
      "upstream": "https://releases.openai.com/codex",
      "channels": ["latest"],
      "default_channel_ttl_seconds": 60,
      "installer_validator": "codex",
      "installers": [
        {"file": "install.sh", "source": "https://releases.openai.com/codex/install.sh", "shell": "sh"},
        {"file": "install.ps1", "source": "https://releases.openai.com/codex/install.ps1", "shell": "powershell"}
      ]
    }
  ]
}
```

该 JSON 是字段示例，不是只注册一个应用的实现清单。实际清单必须同时包含 `anthropic/claude-code`，其上游、latest/stable、签名公钥及 launcher 更新政策按现有模块保持。公钥本体/指纹和算法规则由受审查的协议实现/资源固定，不允许 descriptor 中填写网络下载公钥 URL。

图标、安装说明、更新边界补为有类型的 descriptor 字段，API 返回本地化文本和服务生成的规范 URL。安装命令使用固定模板、安全 origin、已验证 app key 和固定 installer 文件名生成；不下发任意可执行模板。厂商/应用 ID 不重复维护为多个独立真值。

descriptor 首版只描述两应用当前已有的事实：身份、名称/图标、固定上游、渠道、TTL、installer 文件与来源、验证器、更新说明和固定信任版本。不预建 capability DSL、平台继承体系、依赖图、动态配置表单或任意路由/命令模板；前端有需要时只增加两应用实际使用的具体字段。

### 3.4 第三应用接入清单

1. 在 `internal/apps/builtin/manifest.json` 增加唯一 `vendor/app`、固定上游、协议名、渠道、默认 TTL、本地化描述和 installer 描述；按同一规范增加受审查的图标资源。
2. 若上游采用现有协议，复用已有 Protocol；否则新增 `internal/apps/<protocol>/`，实现路径/版本/渠道、metadata 原文与签名验证、制品授权和呈现，再在 builtin 的静态工厂映射中连接一次。新增应用不意味着复制 Catalog 或 Download Manager。
3. 在 `installers/<vendor>/<app>/` 增加原文、来源摘要、许可/固定信任材料、最小 patch 和 generated 脚本；新协议需要新的 installer 语义检查时，在固定验证器映射中加入实现。每日维护从 manifest 自动枚举和计算 allowlist，不再手动更新“应用数”或“脚本数”。
4. 为协议增加少量真实格式 fixture，并把新 descriptor 接入已有参数化契约；至少验证未知资源拒绝、metadata 信任失败、与现有应用同版本/文件名的隔离，以及安装命令的规范根。无新签名协议时不复制一套相同下载/存储测试。
5. 公共目录、详情页、后台应用导航、TTL 表单和 app 指标从 descriptor/API 自动出现。若第三应用出现目前没有的产品行为，届时按真实需求扩展具体字段或组件；首版不为未知行为设计 capability 系统。

正常新增应用只涉及 manifest、协议工厂/必要的新 adapter、应用 installer/资源及对应契约 fixture。HTTP 路由、数据库 schema、清理与下载内核、全局状态组件不因应用数量增加而修改；若实现仍要求这些位置新增应用分支，视为本轮抽象未完成。固定二进制注册需要重新构建/部署，但不会改变已有应用的规范 URL、缓存归属或 DB 主键。旧版本到本设计的首次升级本身允许破坏兼容；这与此后添加第三个独立身份是两件事。

## 4. 身份与 HTTP 路由

### 4.1 规范化和路由顺序

- vendor/app 分别采用 1–63 字符 ASCII 小写 slug：`[a-z0-9]+(?:-[a-z0-9]+)*`。同名 app 可属于不同 vendor；唯一键为二元组。
- 首批保留 vendor：`admin`、`api`、`assets`、`health`。未来增加少量保留名可作为破坏性变更；不为未知未来系统预留大量普通名字。
- 注册时拒绝重复、非规范或保留身份；请求时拒绝非规范大小写、编码斜杠、反斜杠、点段、重复斜杠和二次解码。不将不规范路径静默改写成有效授权。
- 只解码一次。file_path 经公共路径校验后，再由 app adapter 校验其完整语法。禁止把 file_path 直接连接到上游或磁盘目录。
- 先匹配系统路由，再匹配两个身份段。精确 `/<vendor>/<app>` 为详情；有剩余 file_path 为分发。只有已注册应用才有详情。
- `/<vendor>/<app>/` 可用 308 规范化到无尾斜杠的 UI 详情；分发文件请求不依赖重定向。未实现旧路径 alias。
- JSON API/UI 筛选允许各端点列出的 query，拒绝未知/重复参数；分发路径继续拒绝 query。取消目前“任何请求 query 都拒绝”的全局判断，但不泛化下载接口。

### 4.2 路由表

| 方法/路径 | 行为与作用域 |
| --- | --- |
| `GET /` | 公开应用目录；无被要求删除的首页副句 |
| `GET /openai/codex`、`GET /anthropic/claude-code` | 应用详情，Vue Router 直达可用 |
| `GET /<vendor>/<app>/install.sh` 或 `install.ps1` | 对应 generated installer，嵌入当前应用完整规范分发根 |
| `GET /openai/codex/channels/latest` | Codex 协议 channel |
| `GET /openai/codex/releases/<version>/<asset>` | release.json 或已授权制品，按 adapter 解析 |
| `GET /anthropic/claude-code/latest`、`stable` | Claude 原生 channel |
| `GET /anthropic/claude-code/<version>/manifest.json[.sig]` | 签名原文字节/签名 |
| `GET /anthropic/claude-code/<version>/<platform>/<binary>` | manifest 授权制品 |
| `GET /api/bootstrap` | 公开服务版本、站点文案、应用描述、PublicOrigin 和公开 revision 的一致快照；no-store，绝不包含 proxy/admin secrets |
| `GET /api/apps`、`GET /api/apps/<vendor>/<app>` | descriptor 公共投影，包含规范 `id`、详情/分发 URL、installer 与本地化说明 |
| `GET /assets/<bundled-path>` | 编译产物、已注册图标；严格文件白名单 |
| `GET /health/live`、`GET /health/ready` | 存活/就绪 |
| `GET /admin` | UI 重定向到 `/admin/overview` |
| `GET /admin/login` | 登录 UI；return target 只允许路由表内的本地 admin 路由 |
| `GET /admin/overview`、`/admin/events` | 全局后台子路由 |
| `GET /admin/settings/site`、`/admin/settings/proxy` | 全局设置子路由；站点设置页包含公共 URL 独立表单 |
| `GET /admin/apps/<vendor>/<app>/versions`、`.../settings` | 应用后台子路由，应用身份显式 |
| `POST /admin/api/login`、`/logout`、`/password` | 全局认证操作；沿用正确的 Origin/CSRF 边界 |
| `GET /admin/api/session` | 会话/CSRF 引导，不顺带拉 status |
| `GET /admin/api/status` | 仅全局摘要和全局指标，不返回全部应用资源列表 |
| `GET /admin/api/apps/<vendor>/<app>/status` | 明确应用摘要 |
| `GET /admin/api/apps/<vendor>/<app>/versions`、`.../resources` | 应用内列表；有上限和分页/筛选契约 |
| `GET/PUT /admin/api/settings/site`、`.../proxy`、`.../public-url` | 类型化全局设置，分别拥有 revision |
| `GET/PUT /admin/api/apps/<vendor>/<app>/settings` | `channel_ttl_seconds` 与 revision；没有默认应用 |
| `POST /admin/api/apps/<vendor>/<app>/cleanup/preview` | 仅该应用候选，创建冻结 generation 快照 |
| `POST /admin/api/apps/<vendor>/<app>/cleanup/<id>/execute` | 路径 app 必须等于 job app；重复成功执行返回同一结果 |
| `GET /admin/api/events`、`.../apps/<vendor>/<app>/events` | 全局或明确应用的结构化事件；分页上限 |
| `GET /admin/api/history?scope=global&metric=...&range=24h` | global scope |
| `GET /admin/api/apps/<vendor>/<app>/history?metric=...&range=24h` | app scope；metric 必须属于固定定义表 |

认证 cookie 继续限定 `/admin`、HttpOnly、SameSite Strict、按有效 origin 设置 Secure。客户端路由守卫只是 UX，所有管理 API 服务端检查 session；非 GET 写入必须 CSRF 和同源校验。匿名详情可展示，不代表能读管理 API。

列表实现统一采用 `items`/`next_cursor`，默认 50、最多 100；游标绑定应用、端点、筛选条件。资源可按规范版本筛选。版本和事件在 SQL 内限制读取，资源从 manager 快照筛选并限制响应。实时翻页不作为清理授权；清理仍使用独立冻结 generation 预览。

UI fallback 只覆盖表内 UI 路由。未知 app、未知 file、API、签名/二进制请求不能 fallback 成 200 HTML。未知 UI 返回带 404 状态的入口/错误页；未知 API 返回 JSON 404。Vite 的资源基址改为 `/`、输出路径 `/assets/`，不能把现有 `/admin/` asset base 当作 Router base。

服务 origin 仍只支持 scheme+authority，不支持部署子路径。区分安全上下文的 RequestOrigin 与生成链接的 PublicOrigin：前者按请求、受信代理链和部署 allowed_hosts 验证，后者按后台覆盖 > 环境 > RequestOrigin 选择。规范分发根是 `PublicOrigin + "/" + appKey`。PublicOrigin 不能授予新的入站 Host 信任，不能进入上游 URL 授权；Origin/CSRF 和 cookie Secure 依据已验证 RequestOrigin，避免改变发布地址破坏当前管理员的安全上下文。

### 4.3 错误契约

```json
{
  "error": {
    "code": "SETTINGS_REVISION_CONFLICT",
    "message": "Settings changed; reload before saving.",
    "request_id": "example-request-id",
    "retryable": false
  }
}
```

稳定 code 用于前端本地化，不依据英文字符串猜状态。有限且去敏的 details 可包含字段名、app_id；不返回文件绝对路径、上游凭据或内部异常。

| 状态 | 典型 code |
| --- | --- |
| 400 | `INVALID_PATH`、`INVALID_QUERY`、`INVALID_REQUEST`、`INVALID_VERSION` |
| 401/403 | `AUTH_REQUIRED`、`CSRF_REJECTED`、`ORIGIN_REJECTED` |
| 404 | `APPLICATION_NOT_FOUND`、`RESOURCE_NOT_FOUND`、`CLEANUP_NOT_FOUND` |
| 409 | `SETTINGS_REVISION_CONFLICT`、`CLEANUP_EXPIRED`、`CLEANUP_SCOPE_MISMATCH` |
| 429 | `LOGIN_RATE_LIMITED` |
| 502 | `UPSTREAM_UNAVAILABLE`、`METADATA_UNTRUSTED` |
| 503 | `DOWNLOAD_CAPACITY_EXCEEDED`、`LOCAL_STORAGE_UNAVAILABLE` |

不再把 metadata 获取失败伪装成“资源不存在”。已开始流式响应后无法改 JSON/status：中止响应、记录结构化失败，让客户端摘要验证失败，不能写入看似成功的 EOF。

## 5. 配置：单一来源、明确作用域

### 5.1 来源与优先级定案

| 类别 | 内容 | 唯一可编辑来源 | 解析顺序 |
| --- | --- | --- | --- |
| 部署配置 | listen、data_dir、allowed_hosts、trusted_proxies、下载全局限额 | 必须显式提供的 JSON 启动文件；修改后重启 | 有文档的编译默认值 → 文件显式字段 |
| 受信应用定义 | identity、upstream、protocol、trust revision、channels、installer | 编译嵌入 descriptor/adapter | 无运行时覆盖 |
| 全局业务设置 | 站点双语文案、共享出口 proxy | SQLite，经全局管理 API 修改 | 内建默认值 → 已持久化设置 |
| 公共 URL | 服务全局的对外链接 origin | 后台覆盖值；部署环境提供下一级默认值 | 后台持久化覆盖 > `REDAPP_PUBLIC_URL` > 每请求安全 RequestOrigin |
| 应用设置 | channel TTL | SQLite，以规范 app_id 为键 | descriptor 默认值 → 已持久化设置 |
| 浏览器偏好 | locale、非敏感界面偏好 | 浏览器 localStorage/内存 | 合法保存值 → `navigator.languages` 首个支持项 → 列表不可用时 `navigator.language` → en |

运行命令为 `redapp serve --config /etc/redapp/config.json`。只保留新需求明确要求的 `REDAPP_PUBLIC_URL`；不读取其他旧 `REDAPP_*` 或 `--base-url` 等兼容入口，不增设与 JSON 重复的逐字段 env/CLI 覆盖。启动 JSON 不再包含 public_origin，防止出现第四层来源。缺少必需启动文件时失败退出，避免旧部署误用默认数据目录初始化新实例。

```json
{
  "schema_version": 1,
  "listen": ":8080",
  "data_dir": "/var/lib/redapp",
  "allowed_hosts": ["downloads.example.com"],
  "trusted_proxies": ["10.20.0.0/16"],
  "download_limits": {
    "max_active_writers": 16,
    "max_readers": 512,
    "max_artifact_bytes": 4294967296
  }
}
```

示例只含虚构部署参数，无真实凭据。allowed_hosts 是部署认可的有效入站 authority 精确集合（含非默认端口），不是任意 wildcard；由可信代理提供的有效 host 也必须属于该集合。外层代理应覆盖转发头，来自非可信直连 peer 的转发头被忽略。修改公共 URL 不修改 allowed_hosts。健康检查使用配置内已允许的 authority；不为探活放开整个后台 Host 检查。

拒绝未知字段、重复 JSON key、非法范围和错误类型；新配置版本不可降格解释。`redapp config validate --config ...` 只验证并输出去敏结果，不打开/初始化部署数据库。`REDAPP_PUBLIC_URL` 为空视为未提供；非空但非法时启动失败，不能静默降级到请求 Host，即使后台当前有覆盖也须报告部署配置错误。

TTL 默认 60 秒、范围 1–86400；每应用一个 `channel_ttl_seconds`，作用于 descriptor 声明的全部可变渠道。设置更新响应提供 `revision`/ETag，PUT 必须带 If-Match，避免多个标签页静默覆盖。变更 TTL 只影响该应用 channel 新鲜度，不改变可信版本内容。

尚未持久化的设置以 revision 0 返回默认投影，首次成功 PUT 原子插入 revision 1；其后单调递增。撤销公共 URL 覆盖写入 null 并递增 revision，保留设置行，不能删除后回到 revision 0，使旧 If-Match 再次有效。GET 不为默认值创建持久化覆盖。

proxy 仍全局影响所有新上游请求，已建立传输自然结束。密码输入采用显式 keep/replace/clear；读接口只给 has_credentials/has_password，保存成功后清空客户端密码输入。密码在现有单机数据目录权限边界内持久化；不把它复制到公开 descriptor、前端 bootstrap、事件或审计日志。

### 5.2 PUBLIC_URL 更新契约

`GET /admin/api/settings/public-url` 返回 `override_url: string|null`、`environment_url: string|null`、`effective_url`、`source: override|environment|request`、`revision`。request 模式的 effective_url 是本次请求的安全 origin，不存成全局结果。

`PUT` 只接受 `{"override_url":"https://downloads.example.com"}` 或 `{"override_url":null}`，要求 If-Match。表单空字符串转换为 null；它表示撤销覆盖，下一层环境值仍可生效，不另设“忽略环境强制自动”模式。UI 同时显示编辑值、当前有效值和来源。

输入仅允许 HTTP(S) origin：无 credentials、path（可规范化单个尾 `/`）、query、fragment、转义路径及控制/注入字符；host、IPv6、端口按统一安全 parser 校验。PUT 不探测该地址，不请求该地址，也不认为它已配置 DNS/TLS/入站主机。

在同一配置更新锁中 CAS 写 DB revision 后发布不可变内存快照，持久化失败不生效；响应只在新快照可用后成功。并发已开始请求使用其捕获的同一 revision，不能半个响应混用两个 origin。成功返回后开始的新请求、installer 和安装命令立即使用新值，不重启；二进制 Source、授权、metadata、generation、blob identity 均不受影响。

公共 `/api/bootstrap`、`/api/apps` 和动态 installer 响应采用 no-store。JSON 公共响应提供 PublicOrigin 和公开配置 revision；installer 是脚本文本，仅正文内嵌规范分发根，不把 JSON 附加到脚本中。请求推导的结果不跨 Host 缓存。前端保存成功即更新同一 bootstrap store；其他已打开页面在路由进入/重新获得可见性时重新验证公开配置，并按 revision 原子更新命令，不为此在设置页重新引入 status 轮询。已复制到终端的旧命令无法远程改写，UI 不声称能更新它们。

后台发布地址设置不触发自动跳转，不改变当前 router origin，不扩大 cookie 域，不改变上游 Client 或信任公钥。请求安全 origin、发布 origin、上游 origin 是三个独立概念。

## 6. 存储、下载、清理与恢复

### 6.1 三个身份

1. **Logical resource**：`(app_id, canonical_version, resource_key)`；表示某应用某版本某项已授权制品。Codex resource_key 为 asset name；Claude 为 `platform/binary`。
2. **Generation**：随机不可复用 ID；表示一次获取/恢复生命周期。current head 指向精确 generation。清理冻结 generation，不只冻结版本或文件名。
3. **Blob content**：SHA256 和已验证大小；表示完整校验后的内容。按已定案 D2，物理存储与复用 namespace 为 `(app_id, sha256)`，同一应用不同逻辑资源可共享完整 blob；跨应用即使字节相同也各有 blob。

逻辑身份不再由上游 URL+摘要 hash 代替。上游 URL 是不可随请求修改的授权属性，不是业务归属。labels 仅供展示，不再承担授权或查询隔离。

只有完整验证后的内容进入 blob；未完成片段属于 generation。活动下载只按精确 logical resource 合流，不因两个资源 SHA 相同而跨资源/应用合流。完成 blob 重用之前仍必须通过本应用的 metadata 授权并核对期望大小。

### 6.2 核心 schema 3 草案

克制性复核后推荐 **15 张表**，由原 20 张草案合并五处得到；不是以表数为目标，也不新增通用 records 表承接所有业务。逐项裁决如下：

| 原草案表 | 职责与独立必要性 | 本轮裁决 |
| --- | --- | --- |
| schema_version | 识别磁盘格式；已有的一行元数据，改用 PRAGMA 只省名字、不省迁移逻辑 | 保留 |
| app_settings | 每应用 TTL 与 revision；和全局设置一样低频、按 key CAS，无关联查询 | 合并到有明确 scope/key 的 settings |
| service_settings | 站点、代理、公共 URL 的少量类型化文档 | 与 app_settings 共用 settings；Go API 仍按设置类型分开 |
| app_versions | 首次发现历史独立于缓存，旧库也可能只有版本历史 | 保留并承接逐版本累计字段 |
| release_metadata | 大块原始/签名字节及信任状态，和轻量版本统计不同读写节奏 | 保留；不把 raw 放入每次流量更新的版本行 |
| channels | 短 TTL 的可变指向；两个现有模块都已持久化，重启仍可使用未过期缓存 | 保留；改成纯内存会无故改变现有离线/重启行为 |
| resources | 一份 metadata 的多个授权制品；复合键绑定 app/version/key/hash | 保留；与下载尝试、物理 blob 生命周期不同 |
| blobs | 同应用完整内容复用和最后引用 GC | 保留；否则逻辑删除会与物理文件所有权再次混淆 |
| generations | 续传、退休排空、故障恢复所需的具体下载尝试 | 保留；承接 is_current 字段，不保留无限尝试历史 |
| resource_heads | 每个 logical resource 唯一 current 指针 | 删除独立表；generation 的 is_current＋部分唯一索引即可表达 |
| cleanup_jobs | 有限期的清理预览、完成回执 | 改为单表 cleanup_previews；不建立任务执行器 |
| cleanup_items | 从不独立查询的冻结 generation 清单，没有到 generation 的存活外键 | 合入 preview 的类型化 JSON 快照；执行时逐项核对 app/资源 |
| version_stats | 与 app_versions 一对一且同保留周期的两个累计字段 | 并入 app_versions；没有独立表的收益 |
| metric_series | 固定 metric＋scope 的整数 ID 映射 | 删除；直接使用稳定复合键，不为固定目录维护 series 生命周期 |
| metric_counters | 跨重启累计值，不能由保留期有限的样本反推 | 保留，使用 scope/app_id/metric 键 |
| metric_samples | 分钟观测、短期保留、相邻 counter delta | 保留，与小时聚合不同 schema/保留期 |
| metric_hours | 小时聚合、长期保留 | 保留，沿用已验证聚合算法 |
| metric_history_state | 已提交聚合水位，阻止删掉尚未聚合样本及跨时钟回退重记 | 保留现有一行表，不与用户设置混用 |
| events | 有限期、按时间/app 查询的诊断流，缓存删除后仍有意义 | 保留单一事件表，不增加审计/任务/每应用事件表 |
| admin | 密码 hash/revision，认证读写与秘密访问边界独立 | 保留现有单行表，不混入普通设置 JSON |

以下 DDL 定义精简后的主键、外键及核心约束；所有 app_id 写入先验证为 Registry 中的规范身份。时间统一 UTC epoch，单位由字段后缀规定。设置 payload 仅经各类型的 Go 方法读写，验证字段、TTL 范围及秘密投影；不提供业务层可任意 Put 的配置接口。

```sql
PRAGMA foreign_keys=ON;

CREATE TABLE schema_version(version INTEGER NOT NULL CHECK(version=3));

CREATE TABLE settings(
  scope TEXT NOT NULL, app_id TEXT NOT NULL, key TEXT NOT NULL,
  revision INTEGER NOT NULL CHECK(revision>=1),
  payload BLOB NOT NULL,
  PRIMARY KEY(scope,app_id,key),
  CHECK((scope='global' AND app_id='' AND key IN ('site','upstream_proxy','public_url'))
     OR (scope='app' AND app_id<>'' AND key='channel_ttl'))
);
CREATE TABLE app_versions(
  app_id TEXT NOT NULL, version TEXT NOT NULL, first_seen_s INTEGER NOT NULL,
  artifact_requests INTEGER NOT NULL DEFAULT 0 CHECK(artifact_requests>=0),
  downstream_bytes INTEGER NOT NULL DEFAULT 0 CHECK(downstream_bytes>=0),
  PRIMARY KEY(app_id,version)
);
CREATE TABLE release_metadata(
  app_id TEXT NOT NULL, version TEXT NOT NULL,
  raw BLOB NOT NULL, signature BLOB,
  trust_revision INTEGER NOT NULL CHECK(trust_revision>=1),
  fetched_at_s INTEGER NOT NULL,
  PRIMARY KEY(app_id,version),
  FOREIGN KEY(app_id,version) REFERENCES app_versions(app_id,version)
);
CREATE TABLE channels(
  app_id TEXT NOT NULL, channel TEXT NOT NULL, version TEXT NOT NULL,
  fetched_at_s INTEGER NOT NULL, expires_at_s INTEGER NOT NULL,
  PRIMARY KEY(app_id,channel),
  FOREIGN KEY(app_id,version) REFERENCES release_metadata(app_id,version)
);
CREATE TABLE resources(
  app_id TEXT NOT NULL, version TEXT NOT NULL, resource_key TEXT NOT NULL,
  source_url TEXT NOT NULL,
  sha256 TEXT NOT NULL CHECK(length(sha256)=64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
  expected_size INTEGER CHECK(expected_size>=0),
  PRIMARY KEY(app_id,version,resource_key),
  UNIQUE(app_id,version,resource_key,sha256),
  FOREIGN KEY(app_id,version) REFERENCES release_metadata(app_id,version)
);
CREATE TABLE blobs(
  app_id TEXT NOT NULL,
  sha256 TEXT NOT NULL CHECK(length(sha256)=64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
  size_bytes INTEGER NOT NULL CHECK(size_bytes>=0),
  verified_at_s INTEGER NOT NULL,
  PRIMARY KEY(app_id,sha256)
);
CREATE TABLE generations(
  id TEXT PRIMARY KEY,
  app_id TEXT NOT NULL, version TEXT NOT NULL, resource_key TEXT NOT NULL,
  expected_sha256 TEXT NOT NULL, blob_sha256 TEXT,
  phase TEXT NOT NULL CHECK(phase IN ('incomplete','complete','failed')),
  is_current INTEGER NOT NULL DEFAULT 1 CHECK(is_current IN (0,1)),
  retired_at_s INTEGER,
  bytes INTEGER NOT NULL CHECK(bytes>=0), total_bytes INTEGER CHECK(total_bytes>=0),
  source_bytes INTEGER NOT NULL CHECK(source_bytes>=0),
  etag TEXT, resumes INTEGER NOT NULL CHECK(resumes>=0),
  full_retry INTEGER NOT NULL DEFAULT 0 CHECK(full_retry IN (0,1)),
  download_ns INTEGER NOT NULL DEFAULT 0 CHECK(download_ns>=0),
  started_at_s INTEGER NOT NULL, finished_at_s INTEGER,
  verification_ns INTEGER CHECK(verification_ns>=0), last_error_code TEXT,
  CHECK(is_current=0 OR retired_at_s IS NULL),
  CHECK(blob_sha256 IS NULL OR blob_sha256=expected_sha256),
  CHECK((phase='complete' AND blob_sha256 IS NOT NULL) OR (phase<>'complete' AND blob_sha256 IS NULL)),
  FOREIGN KEY(app_id,version,resource_key,expected_sha256)
    REFERENCES resources(app_id,version,resource_key,sha256),
  FOREIGN KEY(app_id,blob_sha256) REFERENCES blobs(app_id,sha256)
);
CREATE UNIQUE INDEX generations_current
  ON generations(app_id,version,resource_key) WHERE is_current=1;
CREATE TABLE cleanup_previews(
  id TEXT PRIMARY KEY, app_id TEXT NOT NULL,
  created_at_s INTEGER NOT NULL, expires_at_s INTEGER NOT NULL,
  selection_json BLOB NOT NULL,
  executed_at_s INTEGER, result_json BLOB
);
CREATE TABLE metric_counters(
  scope TEXT NOT NULL, app_id TEXT NOT NULL, metric TEXT NOT NULL,
  value INTEGER NOT NULL CHECK(value>=0), observed_since_s INTEGER NOT NULL,
  CHECK((scope='global' AND app_id='') OR (scope='app' AND app_id<>'')),
  PRIMARY KEY(scope,app_id,metric)
);
CREATE TABLE metric_samples(
  scope TEXT NOT NULL, app_id TEXT NOT NULL, metric TEXT NOT NULL, t_s INTEGER NOT NULL,
  observed_at_s INTEGER NOT NULL, boot TEXT NOT NULL, value REAL NOT NULL,
  delta REAL, duration_s REAL NOT NULL,
  CHECK((scope='global' AND app_id='') OR (scope='app' AND app_id<>'')),
  PRIMARY KEY(scope,app_id,metric,t_s)
);
CREATE TABLE metric_hours(
  scope TEXT NOT NULL, app_id TEXT NOT NULL, metric TEXT NOT NULL, t_s INTEGER NOT NULL,
  min REAL, max REAL, avg REAL, last REAL, count INTEGER NOT NULL,
  delta REAL, delta_count INTEGER NOT NULL, duration_s REAL NOT NULL,
  CHECK((scope='global' AND app_id='') OR (scope='app' AND app_id<>'')),
  PRIMARY KEY(scope,app_id,metric,t_s)
);
CREATE TABLE metric_history_state(
  id INTEGER PRIMARY KEY CHECK(id=1), aggregated_before_s INTEGER NOT NULL
);
CREATE TABLE events(
  id INTEGER PRIMARY KEY, time_s INTEGER NOT NULL, app_id TEXT,
  version TEXT, resource_key TEXT, generation_id TEXT,
  category TEXT NOT NULL, code TEXT NOT NULL, message TEXT NOT NULL,
  upstream_status INTEGER
);
CREATE TABLE admin(
  id INTEGER PRIMARY KEY CHECK(id=1), hash BLOB NOT NULL, revision INTEGER NOT NULL
);
```

`cleanup_previews.selection_json` 是内部固定结构的数组：generation_id、version、resource_key、snapshot_bytes；app 由 preview 行唯一拥有，不在条目重复保存。它不外键到 generation，也不构成 blob 存活引用：另一个清理先完成后，旧快照仍须安全执行/报告。创建时从该 app 的 current generations 构造；执行前验证所有尚存在条目的 app/version/key 与快照一致，缺失条目视为已回收，不接受客户端提交的任意 selection。保留原有冻结语义，不引入队列、worker、调度或任意任务 payload。事件同样不外键到 generation，避免清理缓存时抹去历史。

current head 是 generation 的 is_current=1 查询，不另存同义指针；替换时在一个事务内把旧行设为非 current/retired，再插入新行，部分唯一索引保证每个 logical resource 最多一个 current。内存索引只是该状态的投影。readers、writer goroutine、锁、singleflight、排队/重试倒计时、瞬时速度和 UI polling 都不持久化；bytes 按现有恢复检查点保存并用实际文件长度校验，不逐 chunk 增加 DB 写入。generation 只保留 current 与仍需排空/恢复的尝试，旧完成/失败尝试在安全释放后删除，诊断留给有保留上限的 events。

DB repository 接口用 `Key` 和 `LogicalResourceKey` 参数，业务层不自行拼 SQL scope。metadata、first_seen、resource bindings 在一个事务中提交；同一规范 release 已可信入库后若上游改变摘要/资源绑定，则报告不可变性冲突，不能静默更新正在服务的 generation。信任规则升级通过 descriptor trust_revision 使 metadata 重新验证，不等同于允许运行时改上游。

### 6.3 文件布局与发布

```text
data/
  state.sqlite
  instance.lock
  objects/parts/<generation-id>.part
  objects/blobs/<sha256(canonical-app-id)>/<content-sha256>.blob
```

DB 不保存任意绝对路径；路径只能由已验证 ID 和固定布局推导，移动整个离线数据目录不会因旧绝对路径失败。目录权限沿用 0700、文件 0600，拒绝符号链接/非普通文件。data 根经实例锁独占；跨文件系统发布不支持。

完成流程：下载片段 → 验证长度和 SHA256 → fsync 文件 → 同目录/同文件系统原子发布 blob（已有同 scope 摘要时核对内容/大小）→ fsync 相关目录 → 短 DB 事务写 blob 与 generation 关联。current head 在 Acquire 创建 generation 时已确定；发布前再次确认未 retired，不让旧 writer 抢回新 head。

同一个 logical resource 在 manager 锁内创建一个 generation，并在短事务中完成旧 current 退休与新 current 插入；已完整校验的同应用 blob 可快速建立新的 complete generation。全局 writers/readers 限额沿用 16/512 默认，metadata 每应用并发默认 32。饱和立即返回有类型的 503；不新增磁盘持久队列、公平调度或自动预下载。

### 6.4 清理与 GC 不变量

- 预览必须带 app，adapter 做该应用版本比较，Unordered 项保留并列出。仅冻结当前 head 的 generation。
- 执行检查路径 app=job app，事务标记选定 generation retired；只有 head 仍指向该 generation 才摘除。重复 execute 返回已保存结果，不重新选择资源。
- reader/writer 活跃时不截断文件、不关闭共享句柄、不复用 generation ID。退休 writer 可以完成旧响应，但不能发布为 current head。
- reader/writer 退出后可移除退休 generation；完整 blob 只有在没有任何 generation 引用、也没有活动 pin 时可回收。不同资源共用同应用 blob 时，同样遵守引用条件。
- 文件删除和 DB 提交不能假装成一个事务。GC 在 manager 锁/短 DB 事务协调下确认无引用，删文件再删无引用 blob 行；删除失败保留可重试记录，启动恢复处理无文件/无引用行。
- 清理预览显示 `selected_generations`、`logical_bytes`、`reclaimable_blob_bytes`、`active_generations`；实际释放值独立记录。即使仅应用内去重，逻辑字节也不必等于立即释放字节。
- 预览有效期 10 分钟；执行结果保留 24 小时供幂等重试，之后删除对应 cleanup_previews 行。缓存清理不删除版本 first_seen、可信 metadata、计数或事件。

### 6.5 启动恢复

拿实例锁 → 确认 schema → 加载 Registry/设置 → 验证路径类型 → 恢复 head/generation → 对完整 blob 重新校验 → 标记中断下载 → 重试安全 GC → 才对外 ready。

blob 已发布但事务未提交时：仅在存在可信 generation 和匹配授权绑定、完整校验通过时接续提交，否则按明确的无引用文件规则回收；不凭文件名创建资源授权。DB 指向丢失/损坏 blob 时将相应缓存标为不可用并摘除 head，记录事件，后续请求新建 generation。恢复后仍使用 adapter 授权和固定上游校验再续传。

原有 Range/ETag/416/摘要验证及 unsafe-resume 新 generation 语义保留。新公共 URL 不要求新增下游任意 Range 功能；上游续传与下游 Range 不混为一谈。

### 6.6 未来演进：全局已验证 blob 去重（首版不实施）

只改变 blob 主键为 `sha256`、路径为 `objects/blobs/<sha256>`、generation 引用和 GC 的全局引用查询；logical resource、授权、generation、app 计数和活动下载合流范围均不变。需要新增同一 blob 被两个应用引用时的清理/恢复测试，并明确应用页报告 logical bytes，global 页报告 unique physical bytes，不能把 app logical bytes 相加宣称全局物理占用。本轮不保留运行时切换两种模式的复杂度。

## 7. 指标、历史与事件契约

### 7.1 已确认的 active 指标与历史保留

指标精简已获二次确认，由独立任务实施：16 项常用与 25 项诊断均继续采集，共 41 项 active；reuse_requests 停止独立计数写入、历史采样及展示，events.recent_total 停止历史采样及展示，但事件详情保留。后续重构接纳该结果，不再要求确认或把已停指标重新启用。tooltip 同属独立任务，接口提供数值、单位、时间、有效性和缺测信息。

active 定义表仅约束新采集/写入。历史表直接使用稳定 metric 文本键，不对 active 清单建立外键或删除级联；已有退役指标样本仍按原周期自然过期，不能因定义减少就主动清库。已知退役键保留读取/聚合所需的 kind/unit 元信息，直到旧样本到期；无法识别的历史键也不能在启动时强制 drop，保留原记录并按所属表原保留期处理，不猜聚合语义。停止展示不等于提前删除数据。

用户另已决定新实例不带入旧历史；源目录历史保留，新实例从自己的观测起点采集。这与现行服务里退役指标历史的自然过期是不同边界。

指标有稳定机器键和明确 scope：`global` 或 `app + canonical app_id`。标签通过前端翻译，不作为存储身份。`versions.total` 定义为应用版本对的数量：global 为 `COUNT(app_versions)`，app 为 `WHERE app_id=?`；两个应用同名版本计 2。严禁再依赖旧 `status.versions`。

AppDefinitions 只能从编译期有限定义表选取具有合理应用语义的指标；内存/进程 uptime/文件系统 free 等不伪装成每应用指标。确切展示/精简清单与 metrics 任务对齐，不在本文额外拍板删除项。时间序列数上界为 `G + A * N`，G 为全局固定定义数、A 为固定 app 定义数、N 为编译注册应用数。version、resource、client IP、事件字符串绝不成为时序维度。

逐版本请求/字节作为 `app_versions` 的累计字段，用于版本页详情，不扩展成无限版本时间序列。事件限总数量和时间保留窗口，使用结构化 `app_id/version/resource_key/generation_id/category/code/upstream_status`；失败资源已回收后仍可识别归属。

### 7.2 计数规则

- artifact 请求确定应用和资源后，global/app 请求计数各加一次；一次请求只能属于一个 AcquisitionKind（miss/cache_hit/shared_follower）。授权失败单独是 API/metadata 错误，不伪装成已开始下载。
- 上游有效 HTTP payload 在读取处累计，包含失败重试消耗；每次消耗 global 与 owning app 各计一次。
- 下游按实际成功写出的字节累计；同一流式写不因更新 snapshot 再计数。
- 清理同时报告逻辑退休量与实际物理回收量；原有 `cleanup_freed_bytes` 保持其已定义语义，若将来新增物理计数使用新稳定键，不能偷偷改旧键含义。
- bounded global/app counters 采用同一短事务成对递增或同一可靠聚合批次，不能一半落库。历史 counter delta 继续识别进程重启和采样间隔。
- 历史采样继续独立于管理页访问；设置页停止轮询不等于停止采集。

错误时序样本是缺测，不写成 0。Overview/图表使用 sample 时间和状态展示陈旧/缺测，不能把某应用 API 失败显示成“该应用资源为零”。

## 8. 前端结构与状态所有权

### 8.1 路由和组件

```text
main.ts：同步 locale 初始化 → createApp → router
RootApp：公共 bootstrap store、统一错误/加载边界
  PublicLayout / AppShell
    HomePage
    ApplicationPage(vendor, app)
  AdminLayout / AppShell
    LoginPage
    OverviewPage
    EventsPage
    SiteSettingsPage（站点文案 + 独立 PublicURLForm）
    ProxySettingsPage
    ApplicationLayout(vendor, app)
      VersionsPage
      ApplicationSettingsPage（TTLForm + CleanupPanel）
```

用 Vue Router history 模式，保留相应代码懒加载。router 对已知内部 UI 链接使用 RouterLink；installer/binary、外站 GitHub、下载链接和页内 anchor 保持各自 HTML 语义。修饰键/中键/新窗口仍可工作，不全局拦截所有 `<a>`。

所有路由参数转为经验证的 Key 后才调用 API，route param 变化可复用组件但必须显式重载和丢弃旧 ticket。`AppShell`、语言、公共配置和认证状态不因每次内部路由变化重建。详情标题、焦点、滚动恢复、404 和浏览器前后退均由路由 lifecycle 管理。

### 8.2 状态归属

| 状态 | 所有者 | 生命周期 |
| --- | --- | --- |
| locale | 根级 locale module/store | 同步初始化；用户选择立即响应并尝试持久化 |
| site/public URL/app 描述 | public bootstrap store | 首次加载、内部保存后合并、路由进入/恢复可见性时去重重验证 |
| session/CSRF | auth store + AdminLayout | 进入后台检查；401、logout、密码变更立即失效并清敏感状态 |
| status snapshot/error/poll timer | 使用该数据的页面 composable | 仅页面活跃且可见时订阅；无重叠请求；离页 abort |
| settings server snapshot/revision | 对应 Form | route app 或设置 key 改变时重新加载 |
| dirty draft/save error | 对应 Form | 与 snapshot 分离；成功保存才替换 baseline；普通 status 刷新无权清除 |
| cleanup preview/execute | CleanupPanel，绑定 app+minimum+job id | app/阈值变化即失效；执行精确 job，不重新选择 |
| password/proxy credential input | 当前表单内存 | 保存、离页、logout 时清空，不入 localStorage 或长期缓存 |

首阶段修复 TTL：每次 app/key 变化同步设置 `{loaded:false,draft:undefined,error:undefined}`，递增 ticket 并 abort 旧请求；GET 成功且 app/ticket 一致才进入 editable。加载失败保留新 app 的空/不可编辑状态，不显示另一应用的旧值。PUT 捕获 app、revision、draft，不在 await 后读取可能已变化的全局 app。409 保留草稿并要求用户比较/重新加载，不自动覆盖。

应用 settings 页面和全局 settings 页面默认不轮询 status；初始化后仅自身 GET/PUT。Overview、Versions、Events 以页面需要的数据轮询（先沿用 5 秒默认，后续按 metrics 任务调整），只启动一个 timer；请求完成后安排下一次，hidden/离页/注销停止，恢复可见性只补一次刷新。采集历史的服务端定时器不受影响。

设置页没有 status-success gate；服务状态读取失败不能阻止编辑独立可用的设置。短 session 检查可在后台 layout 激活/窗口恢复时执行，不能用全量 status 代替认证检查。401 时可关闭表单并清凭据，不能把此安全动作与普通轮询覆盖 draft 混为一谈。

dirty 表单离开页面或切换 app 时提示放弃/留在当前页；不自动保存。必要的页面非敏感草稿可在当前会话内保存，但不引入默认持久化草稿功能。删除/清理有独立预览确认，不用全局“正在刷新”状态替代操作进度。

### 8.3 locale 与站点 bootstrap

优先级已定案，不再送审：

1. `localStorage` 的合法手动选择（仅 `en`、`zh-CN`）。
2. 按 `navigator.languages` 顺序找首个支持语言：`en` 及合法 en-* → en；`zh` 及合法 zh-*（含 Hans/Hant/CN/TW/HK）→ 当前唯一中文包 zh-CN。
3. languages 不存在、不可访问或为空时，检查 navigator.language，使用同样映射。
4. 列表存在但无支持项，或上述均不可用时，回退 en。无效 token 跳过；不解析 HTTP q 权重或 `*`。

在 createApp/mount 和路由组件渲染前得到 locale，并设置 documentElement.lang。只有用户明确切换才写入“手动选择”storage；自动推断不写成手动选择，否则浏览器偏好后续变更会被旧推断值永久遮蔽。storage 读取/写入异常都只影响持久化，不阻止页面使用。

不引入服务端 Accept-Language bootstrap 或相应 Vary。静态 HTML 保持无语言正文的中性加载结构；可访问的加载/失败文本由同步 locale 模块在首帧产生。应用失败边界也必须已本地化。

异步站点文案与同步 locale 分开：根级 public bootstrap request 加有限超时/取消，未返回时 header/footer 自定义文案区域展示局部骨架，不先显示一套会迅速被替换的默认品牌。其余必要内容可按就绪情况显示，不能全站无限空白。失败后显示同一 locale 的本地默认站点文案和重试提示。

定案为单次 `GET /api/bootstrap`：返回版本、site、applications、PublicOrigin 和公开 revision 的一致快照，替代 `/api/info`；不保留旧 info alias。`/api/apps` 仅作为明确的目录 API，不是前端初始化第二次必需请求。公开 revision 来自构建 descriptor 身份、site revision、public URL revision 和本次有效 PublicOrigin，不能拿 revision 相等作为跨 Host 共享响应的许可。

安装命令只使用 bootstrap store 已确认的 PublicOrigin，不用未验证 window.location 或另一份旧 origin 拼接。失败时可以展示本地化说明和重试，但不能把猜出的地址当成已加载的安装命令。保存 site/public URL 时，根 store 接受成功响应的新值并使旧 bootstrap ticket 失效；迟到的初始响应不能覆盖刚保存的配置。

整页英文→中文闪现尚未复现。既有代码 i18n 在模块求值时同步初始化，原 `site` 默认文案也使用所选 locale；已证实的是默认站点文案→服务器自定义文案的替换。因此测试针对首帧 locale/文案/命令和延迟响应，而不是把未经证实的异步语言恢复当作根因。

### 8.4 下拉基础层

抽 `Popover`（定位、dismiss、Escape、focus return、唯一 id）和共享 trigger/panel/item CSS tokens。统一 header 触发器高度、padding、边框、阴影、gap、逻辑方向对齐、z-index 和最大可用高度；弹层最小宽度可按内容不同。

`SelectMenu` 继续是 combobox/listbox/option，焦点留触发器，typeahead 和 active descendant；`AccountMenu` 继续是 menu/menuitem，移动实际焦点，执行动作。语义行为由两种组件分别实现，不把账号操作塞进 selection value。已有 outside click、Tab、Home/End、Escape、禁用、焦点回归测试必须保留并扩展至两个弹层相邻打开/关闭情况。

## 9. Installer 与每日维护

### 9.1 Descriptor 是唯一清单

installer 目录变成 `installers/openai/codex/`、`installers/anthropic/claude-code/`，路径由受验证规范 ID 和固定目录格式派生。共享 embed/render 层按 app key 和 descriptor 声明的文件名选择脚本；不存在默认 Codex 和任意文件读取。

服务端渲染只替换受审查 placeholder 为本请求完整规范分发根，例如 `https://downloads.example.com/anthropic/claude-code`。原文和签名资源保持逐字节身份；patch 仍是最小可审查差异，generated 必须严格等于 original+patch。

从同一 builtin manifest 生成 inventory、预期文件集合、官方源、测试/验证器关联和 publish allowlist；不再出现固定 APPS 二元组、`len==4`、`else Claude` 或各脚本重复维护应用列表。validator 名称只能映射受审查的仓库实现；调用用固定 argv，不执行 descriptor 提供的 shell 字符串。

### 9.2 流水线边界

1. 固定 main commit，用只读权限读取受审查 descriptor/provenance；遍历全部声明 installer，任何检查失败都汇总失败，不能当作 unchanged。
2. 只从 descriptor 的官方 HTTPS 源获取；每跳验证 scheme/credentials/fragment、大小/时间/重定向上限和实际连接目的地。下载阶段不执行内容，不继承任意代理环境改变安全边界。
3. 新脚本严格零 fuzz/offset patch；在无网络、无凭据、只读源码挂载、资源限制的容器中执行受审查验证器。脚本变化需要相应平台 parser，不可缺失时静默跳过。
4. 可信宿主重新应用 patch，逐字节比对容器产物，再打包允许变化的 upstream/generated/provenance。patch、公钥、许可证、descriptor、workflow、产品代码都不能由 daily 更新。
5. 写权限 job 固定同一 baseline，验证包摘要/大小/路径/文件集合；拒绝人工改过的托管分支或非草稿 PR，使用普通 fast-forward 更新。只创建/更新草稿，不自动 merge/release/deploy。
6. 新应用的加入本身属于人工受审查代码变更；每日维护仅更新已注册应用的官方脚本基线。不能因 genericity 获得添加应用或任意执行插件的能力。

现有 Codex 25、Claude Shell 64 离线场景、PowerShell/Windows 测试与维护 15 场景作为基础；目录改名不能令它们被遗漏。真实官方二进制运行与各原生平台仍是独立验收，不用 fixture 成功代替。

## 10. 仅使用全新目录

不带入旧配置、下载缓存或历史。部署者显式选择新 data_dir，重新配置站点、代理和后台密码，缓存按请求重新获取。旧目录保留用于归档或切回旧版；新实例不修改它，也不反向同步数据。

启动在创建锁、数据库或执行修改性 PRAGMA 之前，只读检查目标：不存在/空目录允许初始化；属于本架构 schema 3 的目录允许恢复；旧 schema、损坏数据库、非空未知目录均拒绝启动并给出“选择全新数据目录”的引导。不提供自动升级、重建、清空、迁移或导入开关。

实现验证使用临时旧版 fixture，确认失败启动不会改变原 DB、缓存或目录内容。新实例的指标、事件和首次发现时间从本次运行开始；旧服务已有退役指标样本仍遵循其自然过期策略，不因本重构主动删除。

## 11. 分阶段实施与验收

### 阶段 0：已有缺陷与并行任务合并纪律

先在现有行为上落小修，避免重构长期掩盖问题：TTL failed-switch、global versions、设置页不轮询及 error ownership。接入已有首页文案删除和已确认的指标精简结果；不重复实施 metrics tooltip，不扩大停采名单。这些修复先于核心重构交付验证。

验收：已有 33 个 DOM 测试/Go race 保持通过；本次三个复现转为通过的回归；设置 GET 失败不能提交其他 app 值；同名版本全局计 2；设置页无无关 status timer、错误不被其他成功请求抹去，dirty 输入保持。

### 阶段 1：身份、注册、协议适配和配置

先交 Key/Registry/manifest、两 adapter、catalog service、typed config/API contract。固定 URL 表和 PUBLIC_URL precedence，不增加默认应用。生成 OpenAPI 或同等共享类型契约，前后端不分别手写两应用枚举。D3/D4 可直接按推荐执行。

验收：注册错误在启动前失败；metadata 签名/原文/上游边界与两个旧模块等价；无 app 参数的应用 API 不可用；三层公共 URL 优先级/清空覆盖/revision/即时生效均有测试。

### 阶段 2：schema 3 与共享下载存储

按已定案的应用内 blob 复用实现 repositories、逻辑 resource/current generation/blob、清理/恢复。先用新空目录测试；不运行真实迁移。保留现有流式读取、续传、安全重试和 SIGKILL/crash 注入测试。

验收：同版本/同文件名跨 app 隔离；同 app 同 blob 复用；不同 app 同 source/hash 不再逻辑冲突；清理 A 不改变 B 的 head/reader/数据；旧快照不删除新 generation；无引用 GC、DB失败和文件失败可恢复；指标 scope 原子计数正确。

### 阶段 3：HTTP 规范路径、installer 描述治理与前端完整 SPA

后端实现明确 UI/API/distribution 解析，无旧 alias；installer 生成规范公共根。前端同阶段切换 Router、layout、bootstrap、页面订阅、独立表单、Popover tokens。PUBLIC_URL 表单采用已定优先级；不因 SPA 顺手开放上游、放宽所有 query 或修改签名。

验收：直达/刷新/前后退可用；API/下载不落入 HTML fallback；安装命令与实际 handler 匹配；资源懒加载失败有本地化重试；下拉语义和焦点测试通过；settings 无 polling、hidden/leave/logout 清理请求；首页删除句子不再出现。

### 阶段 4：全新目录与交付门禁

交付新目录初始化、重新配置、缓存重建及旧目录归档说明。不实现迁移工具。用临时旧目录验证拒绝启动且源内容不变，再验证 schema 3 重启恢复。更新运维和 Docker 配置契约，运行适用的 CLI、协议、存储、DOM 和平台门禁；发布由父任务协调，本轮不推送或部署。

### 验收矩阵

该矩阵是行为覆盖清单，不是“每格创建一个测试文件”的要求。实现按三层组织：

1. 少量真实关键流程：启动/认证/公共分发、同资源并发、清理与重启、旧目录拒绝；复用本地固定上游和临时 DB，让 HTTP、Catalog、Store、Download 真正协同。
2. 模块契约：ID/路径、metadata/signature、配置优先级、schema scope、adapter comparison 等采用表驱动/参数化输入。验证外部行为，不镜像实现步骤和每个私有 helper。
3. 高风险回归：保留签名、跨应用隔离、恢复/续传、清理崩溃窗口与三个已复现缺陷。UI 只对路由生命周期、draft 归属、异步竞争、键盘可访问性做必要 DOM/mock 测试；避免整页文案快照和大量 CSS/内部 DOM 结构断言。

同一信任校验在最合适层做穷举，HTTP 层仅保留少量真实接线验证，不把同一失败矩阵在 Go/Python/DOM 重复一遍。新架构不再承诺的旧 URL/env/default-Codex 兼容测试应先列出、在对应替代契约完成后调整；此文档不是删除现有测试的授权，不能把“减少数量”作为削弱安全门禁的理由。进展报告按行为覆盖、失败和未验证环境组织，测试计数只作辅助。

| 领域 | 必须覆盖的正/反例 |
| --- | --- |
| Identity | 两规范 ID；不同 vendor 同 app 名；unknown/reserved/uppercase/空段/点段/编码斜杠/重复斜杠；DB 与 API 一致 |
| Trust | Claude 原始签名字节、错误 key/算法/过期/损坏；Codex 非授权 URL/摘要；重复 JSON key；信任 revision 失效 |
| Metadata | 两应用同版本/渠道并发 singleflight；TTL 独立、失败不回退另一 app/stale channel；version immutable conflict |
| Downloads | 同逻辑资源 100 followers、全局 reader/writer 上限、迟到/慢/取消 reader、长度/hash失败、Range/ETag/416/unsafe retry |
| Storage | 同 app 多逻辑资源共享 blob；跨 app 相同内容无 logical collision；路径搬迁、符号链接拒绝、文件/DB故障、崩溃恢复 |
| Cleanup | app/job匹配、过期、幂等、旧预览、新generation、reader/writer排空、共享blob最后引用、GC重试、历史不被删 |
| Config | 缺配置/未知字段/重复键/非法限额；app TTL scope/revision；proxy原子切换；秘密不出公开API |
| Public URL | DB>env>request、null恢复env、无env恢复request、非法origin、无子路径/注入、失败不生效、并发revision、缓存不串Host、source展示 |
| Origin/Auth | 非可信代理头忽略、可信链错位拒绝、allowed host边界、CSRF/Origin、PUBLIC_URL更新不改upstream/Host许可/cookie domain |
| API/Routes | exact详情与file区分、白名单query、非法app404、API错误JSON、下载非HTML、旧路径无alias、HEAD/POST等按契约拒绝 |
| Metrics | 41项active及16/25分组、两个退役项按已批准范围停写/停展示、旧样本自然过期、未知历史不强删；scope稳定、global/app单次计数、versions同名计2、新写入拒绝未知metric、gap不假装0 |
| Frontend drafts | failed-switch旧值清除、迟到响应丢弃、保存app捕获、409留草稿、dirty离页、密码清理、无status gate |
| Polling | 仅页面订阅、settings零status轮询、hidden暂停、恢复一次、无重叠、401/logout/unmount停止、状态错误不清表单错误 |
| SPA | 所有UI直达/刷新、前后退、app参数变化、新窗口、标题/焦点/滚动、chunk failure retry、404正确 |
| Locale | saved优先、languages顺序、en-US/GB、zh-CN/TW/HK/Hans/Hant、unsupported/malformed、空/不可用列表、storage异常、首帧HTMLlang/正文一致 |
| Site bootstrap | 慢/失败请求局部骨架与fallback、revision旧响应不覆盖新保存、公共URL保存后命令更新、不出现全局无限空白 |
| Popover | Select与Menu不同ARIA、typeahead/箭头/Home/End/Tab/Escape/outsideclick、focus return、disable、相邻弹层、逻辑对齐 |
| Installer maintenance | descriptor全覆盖、摘要/patch冲突、parser缺失失败、离线容器、独立宿主重验、动态生成allowlist、包篡改/路径穿越、PR人工修改保护 |
| Fresh directory | 全新目录初始化、旧 schema/非空未知目录拒绝且源不变、无迁移入口、新 schema 重启恢复 |

## 12. 代码依据、验证边界与实施检查

固定证据链接：

- [静态装配与旧配置](https://github.com/PMExtra/RedApp/blob/88c789e023597774a1ca1b812887b46b59a3f49c/cmd/redapp/main.go#L73-L125)
- [原资源身份与 Acquire 边界](https://github.com/PMExtra/RedApp/blob/88c789e023597774a1ca1b812887b46b59a3f49c/internal/download/manager.go#L266-L345)
- [原清理快照语义](https://github.com/PMExtra/RedApp/blob/88c789e023597774a1ca1b812887b46b59a3f49c/internal/download/manager.go#L790-L847)
- [原 schema 迁移](https://github.com/PMExtra/RedApp/blob/88c789e023597774a1ca1b812887b46b59a3f49c/internal/store/store.go#L23-L58)
- [已有渠道持久化读取](https://github.com/PMExtra/RedApp/blob/88c789e023597774a1ca1b812887b46b59a3f49c/internal/apps/claude/catalog.go#L131-L139)、[Codex latest 持久化](https://github.com/PMExtra/RedApp/blob/88c789e023597774a1ca1b812887b46b59a3f49c/internal/apps/codex/catalog.go#L221-L224)
- [聚合水位与原始样本删除事务](https://github.com/PMExtra/RedApp/blob/88c789e023597774a1ca1b812887b46b59a3f49c/internal/history/history.go#L108-L136)
- [TTL 失败切换缺口](https://github.com/PMExtra/RedApp/blob/88c789e023597774a1ca1b812887b46b59a3f49c/frontend/src/components/Maintenance.vue#L7-L64)
- [global versions 口径错误](https://github.com/PMExtra/RedApp/blob/88c789e023597774a1ca1b812887b46b59a3f49c/internal/httpserver/history.go#L58)
- [全局 polling 与 error 共用](https://github.com/PMExtra/RedApp/blob/88c789e023597774a1ca1b812887b46b59a3f49c/frontend/src/App.vue#L75-L106)
- [原同步 locale 初始化](https://github.com/PMExtra/RedApp/blob/88c789e023597774a1ca1b812887b46b59a3f49c/frontend/src/i18n.ts#L3-L27)
- [单入口按初始 pathname 选择组件](https://github.com/PMExtra/RedApp/blob/88c789e023597774a1ca1b812887b46b59a3f49c/frontend/src/main.ts#L1-L6)
- [原文/patch 与维护验证](https://github.com/PMExtra/RedApp/blob/88c789e023597774a1ca1b812887b46b59a3f49c/scripts/installer_maintenance.py#L139-L198)

本设计基于前一轮实际代码/隔离测试：原 Go race/vet、33 个 DOM、installer/maintenance/CLI 测试通过；三个额外正确性断言复现已列缺陷；保存中文首帧已中文的测试通过。当前分支已另外执行整仓 race/vet、34 项 DOM、生产构建与真实 CLI 验证，详见[本地验证记录](multi-application-validation.md)。实际 schema 以 internal/store/schema.sql 为准；旧基线结果不替代新分支检查。

原 20 表草案的约束实验只适用于已被替换的草案，不能当作当前精简设计的验证。15 表实际实现已通过新格式初始化、类型化设置、授权、计数、清理、并发/恢复及隔离测试。TTL 范围和 cleanup JSON 条目归属由类型化方法检查；不开发旧版迁移。

仍不声称验证了：真实生产流量、官方二进制运行、所有客户端实机、daily 有变化分支真实 PR 权限、GUI/辅助技术体验或用户所见整页英文闪现。实现时按上述阶段逐项记录实际验证环境和结果。

评估工量：按一名熟悉当前 Go/Vue 的开发者、复用已有测试和协议模块，核心重构+规范路由/SPA/配置/installer整合约 2–3 工作周；不包含旧配置、缓存或历史迁移。并行分工可缩短历时但不按人数线性缩短。平台门禁和未知旧数据情况会改变范围；阶段0小修应先独立交付。全局去重不计入首版范围。此为范围估算，不是发布日期承诺。
