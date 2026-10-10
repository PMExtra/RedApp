# HTTP API 契约

[`api/openapi.yaml`](../../api/openapi.yaml)（OpenAPI 3.1）是 RedApp 全部 HTTP 路由的唯一来源。服务端按它实现，前端由它生成类型和客户端，契约测试按它校验真实响应。改接口时先改规范，再改实现，两者在同一个 PR 中提交（[ADR 0009](adr/0009-openapi-contract.md)）。

## 规范的组织

| 部分 | 内容 |
| --- | --- |
| `info.description` | 全局规则：方法语义、路径与查询校验、JSON 严格解析、`X-Request-Id`、安全响应头、错误、revision、分页、认证 |
| `tags` | 按领域分组：health、public、pages、assets、distribution、auth、overview、settings、directory、configuration、exchange、releases、cache、prewarm、hosted |
| `x-spa-routes` | 返回 SPA 文档的深链白名单（服务端为 `spa.go` 的 `adminSPARoutes`） |
| `paths` | 103 个操作；分发、页面、静态资源也在其中 |
| `components.schemas` | 全部请求/响应模型，响应对象均为 `additionalProperties: false` |
| `components.x-error-codes` | 错误码目录：HTTP 状态、`retryable`、含义 |
| `components.x-error-code-groups` | 按条件自动适用的错误码组（见下文） |
| `components.examples` | 配置覆盖、导入预览等复杂响应示例；请求示例写在各操作中 |

操作上的扩展字段：

| 扩展 | 含义 |
| --- | --- |
| `x-error-codes` | 该操作特有的错误码 |
| `x-max-body-bytes` | 请求体上限，超出返回 `413 PAYLOAD_TOO_LARGE` |
| `x-greedy` | 路径参数可以包含 `/`，匹配路径剩余部分 |
| `x-distribution-routes` | 各 Provider 在 `/{vendor}/{app}/{file_path}` 下的具体路径、媒体类型和行为 |
| `x-range-support`、`x-streaming`、`x-timeout`、`x-idempotency`、`x-polling`、`x-limits`、`x-sandbox` | 实现时必须遵守的行为约束 |
| `x-multipart-order` | multipart 字段的顺序（流式上传要求小字段在文件前） |

## 设计规则

- **路径**：公开接口 `/api/`，管理接口 `/admin/api/`，应用以 `/apps/{vendor}/{app}` 定位。集合用复数名词，动作是子路径上的 POST（`.../preview`、`.../execute`、`.../cancel`）。
- **方法与状态码**：GET 读；PUT 整体替换设置文档；PATCH 稀疏修改（实体上只改 `enabled`，配置用 `set`/`unset`）；POST 创建返回 `201`，动作返回 `200`（异步启动返回 `202`）；DELETE 和无内容的动作返回 `204`。
- **字段**：全部 `snake_case`；时间是 RFC 3339 UTC；缺失值按 schema 用 `null` 或省略，列表不为 `null`。只与某些 Provider 相关的字段（`base_url`、`base_urls`、`source_strategy`、`cache_ttl_seconds`、`http_policy`、`retention`、`prewarm`）只在适用时出现。
- **并发控制**：每个可编辑资源返回 `revision` 和 `ETag: "<revision>"`。所有写操作必须带 `If-Match`，缺失或格式错误返回 `400 IF_MATCH_REQUIRED`，过期返回 `409 REVISION_CONFLICT`。请求体不再携带 `revision`。按 UID 防止误伤同名重建对象的守卫（`confirm_uid`、`source_uid`、`notes_revision`）不匹配时也返回 `409 REVISION_CONFLICT`。选择 409 而不是 HTTP 标准的 412，是为了让“过期草稿”只有一个错误码，前端统一保留草稿并重新加载。
- **不需要 `If-Match` 的写操作**：登录/登出/改密码、上传图标、导入导出、对预览或任务 ID 的动作（ID 已绑定冻结的状态）、托管文件删除（文件 ID 不可变，替换用 `expected_id`）、HTTP 缓存单文件刷新与路径匹配测试。依赖已保存配置的动作（保留预览、复制应用）要求 `If-Match`。
- **分页**：无限增长的列表（事件、版本、资源、缓存条目、预览条目）用游标 `limit`/`cursor` → `items`/`next_cursor`；需要页码的有限列表（厂商、应用、分类、托管文件、保留与预热条目）用 `page`/`limit` → `items`/`page`/`limit`/`total`/`total_pages`。`limit` 最大 100，默认值写在各操作。页码超出时返回空 `items`，不再回退到最后一页。
- **禁用的应用**：需要已启用应用的动作（保留、预热等）在应用或其厂商被禁用时返回 409 `APPLICATION_DISABLED`（不可重试）；其他工作包遇到同样情况复用这个错误码，不用 `SOURCE_CHANGED`。
- **错误**：响应体固定为 `{"error": {"code", "message", "request_id", "retryable"}}`，每个场景有显式 `code`，前端只按 `code` 判断。`retryable` 由目录决定，不由 HTTP 状态推导。

### 错误码的归属

一个操作可能返回的错误码 = 它的 `x-error-codes` ∪ 所有条件成立的组：

| 组 | 条件 | 错误码 |
| --- | --- | --- |
| `common` | 所有操作 | `INVALID_PATH`、`INVALID_QUERY`、`REQUEST_ORIGIN_INVALID`、`INTERNAL_ERROR`、`STORAGE_UNAVAILABLE` |
| `admin_origin` | 路径以 `/admin/api/` 开头 | `ORIGIN_REJECTED` |
| `session` | `security` 需要 `sessionCookie` | `AUTH_REQUIRED` |
| `csrf` | `security` 需要 `csrfToken` | `CSRF_REJECTED` |
| `json_body` / `multipart_body` | 有对应类型的请求体 | `INVALID_REQUEST`、`UNSUPPORTED_MEDIA_TYPE`、`PAYLOAD_TOO_LARGE` |
| `if_match` | 声明了 `If-Match` 参数 | `IF_MATCH_REQUIRED`、`REVISION_CONFLICT` |

路由层（尚未选中操作）只会返回 `x-router-error-codes`：`NOT_FOUND`、`METHOD_NOT_ALLOWED`（带 `Allow`）、`INVALID_PATH`、`REQUEST_ORIGIN_INVALID`。

## 服务端实现

实现结构（构造、路由表、中间件顺序、错误与请求辅助、测试工厂、契约校验）见 [architecture.md](architecture.md#http-层)。

- 每个操作在 `internal/httpserver/routes.go` 的路由表中占一行，用 `http.ServeMux` 的方法 + 路径模式注册；`x-greedy` 参数用 `{name...}`，`/` 用 `/{$}`。GET 模式同时应答 HEAD，所以规范中的 HEAD 操作和 `install.sh`/`install.ps1` 不单独注册（路由表用 `servedBy` 标明由哪个操作的注册应答）；安装脚本若单独注册，会与 `/api/vendors/{vendor}`、`/admin/vendors/{vendor}` 等三段路径互相冲突。
- `/admin/{ui_path}` 与 `/{vendor}/{app}` 系列在 ServeMux 中互不包含，不能直接注册：按 `x-spa-routes` 逐条注册后台页面；其余 `/admin/...` 落到 `/{vendor}/...` 处理器，由它识别保留厂商名后返回后台文档并带 404（`/admin/api/...` 返回 `404 NOT_FOUND`）。其他保留厂商名（`api`、`assets`、`health`、`all`）返回 `404 NOT_FOUND`，不进入应用查找。
- 页面文档按 `x-spa-routes.documents`：公开页面（`/`、`/all`、`/{vendor}`、`/{vendor}/{app}`）返回 `index.html`；`/admin/...` 全部返回 `admin.html`，从不回退到 `index.html`。前端构建缺少某个入口时该类页面返回 `500 INTERNAL_ERROR`（当前提交的旧前端只有 `index.html`，后台页面因此不可用，直到重写的前端产物提交）。文件都在构建目录根部（`index.html`、`admin.html`、`assets/`），布局只在 `spa.go` 的常量中定义。
- 厂商或应用不存在、未发布或标识无效时，`/{vendor}` 和 `/{vendor}/{app}` 返回 `index.html` 并带 404，由 SPA 显示“页面不存在”；其下的分发路径仍返回规范中的 JSON 错误。首段为 `api`、`assets`、`health` 的未知路径返回 `404 NOT_FOUND`。
- 查询参数、请求体上限和鉴权按路由声明，由中间件执行；`TestRouteTableMatchesSpec` 保证与规范一致。JSON 严格解码（`jsoncheck.Strict`）、`If-Match` 解析和错误码都是共享辅助函数。
- 错误码常量在 `error_codes.go`，与 `components.x-error-codes` 由测试保持一致；状态和 `retryable` 只来自这张表。
- 契约测试：`newHarness` 的每个响应都按规范校验（状态、`X-Request-Id`、安全头、声明的响应头、媒体类型、JSON Schema 2020-12 响应体、错误码归属与目录一致）；`spec_test.go` 检查路由表、`x-spa-routes`、错误码目录和规范自身的结构。

### 迁移状态

已按规范实现：health、public、pages、assets、distribution、auth 六个标签下的全部 29 个操作，以及工作包 2 的 21 个管理操作（发布版本、资源、版本清理、保留、预热、托管文件管理）。以下差异已落地：错误码与 `request_id`、`Error` 文档（含 404/405）、HEAD（发布制品不触发下载）、认证与会话、公开 API、分发与静态资源、页码超出时返回空页、`PREWARM_BUSY` 改为标准 `Error`，以及下文“发布类应用”“预热”“托管文件”三节。

工作包 1（directory、configuration、exchange 三个标签的 27 个操作）已按规范实现，下文“目录、配置与分类”“导入导出与复制”两节的差异均已落地；旧的 `settings`、`instructions`、`assets/icons`、`assets/builtin-icon` 与 `vendors/{v}/apps` 路径已删除。`legacy_directory.go` 的分派函数直接返回 `false`，该文件只保留工作包 2、3 的旧处理器仍在使用的 `directoryError`、`positivePage`。

其余 47 个管理操作在路由表中标记为 `legacy`，仍由旧处理器以旧路径和旧响应形状服务（`legacy.go` 的分发骨架与 `legacy_releases.go`、`legacy_overview.go` 两个分派函数）；规范已删除的旧路径经 `/admin/api/` 兜底注册到达。三个工作包互不重叠：

| 工作包 | 操作 | 主要文件 |
| --- | --- | --- |
| 1 目录、配置、导入导出、分类、管理备注（27，已完成） | `listProviders`、`listVendors`、`createVendor`、`getVendor`、`updateVendor`、`deleteVendor`、`listApps`、`createApp`、`getApp`、`updateApp`、`deleteApp`、`uploadIcon`、`getVendorConfiguration`、`patchVendorConfiguration`、`getAppConfiguration`、`patchAppConfiguration`、`getVendorNotes`、`replaceVendorNotes`、`getAppNotes`、`replaceAppNotes`、`listCategories`、`getCategory`、`patchCategory`、`exportConfiguration`、`previewImport`、`executeImport`、`copyApp` | `directory.go`、`directory_listing.go`、`directory_table.go`、`configuration.go`、`admin_notes.go`、`taxonomy.go`、`exchange.go`、`proxy_redaction.go`、`application_work.go`、`legacy_directory.go`；测试 `directory_test.go`、`directory_table_test.go`、`configuration_test.go`、`admin_notes_test.go`、`taxonomy_test.go`、`exchange_test.go`、`vendor_icons_test.go`、`proxy_redaction_test.go`、`force_delete_test.go`、`scoped_proxy_test.go`、`v072_test.go` |
| 2 发布版本、资源、版本清理、保留、预热、托管文件管理（21） | `listVersions`、`listResources`、`previewVersionCleanup`、`executeVersionCleanup`、`getRetentionStatus`、`previewRetention`、`getRetentionPreview`、`listRetentionPreviewItems`、`executeRetention`、`getPrewarmOptions`、`startPrewarm`、`getPrewarmJob`、`listPrewarmItems`、`cancelPrewarmJob`、`retryPrewarmJob`、`listHostedFiles`、`uploadHostedFile`、`importHostedFile`、`deleteHostedFile`、`getHostedTransfer`、`cancelHostedTransfer` | `listing.go`、`numbered_listing.go`、`retention.go`、`prewarm.go`、`hosted.go`、`legacy_releases.go`；测试 `listing_test.go`、`retention_test.go`、`prewarm_test.go`、`content_providers_test.go` |
| 3 HTTP 缓存管理、概览、事件、历史、站点设置（26） | `listSources`、`listCacheEntries`、`refreshCacheEntry`、`previewCacheRefresh`、`getCacheRefresh`、`listCacheRefreshItems`、`executeCacheRefresh`、`previewCacheCleanup`、`getCacheCleanup`、`listCacheCleanupItems`、`executeCacheCleanup`、`getAutoCleanupStatus`、`testPathMatch`、`getStatus`、`getHistory`、`listEvents`、`getAppStatus`、`getAppHistory`、`getSiteSettings`、`replaceSiteSettings`、`getPublicUrlSettings`、`replacePublicUrlSettings`、`getGlobalProxySettings`、`replaceGlobalProxySettings`、`getHomepageSettings`、`replaceHomepageSettings` | `cache.go`、`status.go`、`history.go`、`homepage_settings.go`、`legacy_overview.go`；`listing.go` 中的 `eventList`；测试 `cache_policy_test.go`、`cache_capacity_test.go`、`general_routes_test.go`（管理部分）、`dynamic_metrics_test.go`、`site_test.go` |

每个工作包的做法：把本包在 `routeTable()` 中的行从 `serve: s.legacyAdmin, …, legacy: true` 改为新处理函数（契约测试随之生效），按规范重写处理器与测试；完成后让本包的 `legacy_*.go` 分派函数直接返回 `false` 并删除不再引用的旧处理器。不要修改 `legacy.go`；三个包都完成后，在一次清理中删除 `legacy.go`、三个 `legacy_*.go`、`legacyCatchAll` 和路由字段 `legacy`。

## 阶段 4：前端

- 用 `openapi-typescript` 从规范生成类型，用 `openapi-fetch` 作为客户端；不手写 DTO。生成物与规范一起提交，CI 检查可复现。
- TanStack Query 的 key 以 operationId 加参数构成；写操作成功后用响应体替换缓存（响应总是完整的新状态和新 `ETag`）。
- 409 `REVISION_CONFLICT` 保留草稿并提示重新加载；`retryable: true` 的错误可提供重试。
- MSW 的模拟响应使用生成的类型，示例可直接取自规范。

## 与现有实现的差异

以下改动是有意的，3b 和 4 以此为准。未列出的接口保持原有语义。已落地的部分见[迁移状态](#迁移状态)。

### 全局

| 项 | 现有 | 规范 |
| --- | --- | --- |
| 错误码 | `fail()` 按状态推导，所有 409 都是 `SETTINGS_REVISION_CONFLICT`，所有 403 都是 `CSRF_REJECTED` | 每个场景显式 `code`，见目录 |
| 改名/拆分的错误码 | `SETTINGS_REVISION_CONFLICT`、`DIRECTORY_REVISION_CONFLICT` | `REVISION_CONFLICT` |
| | `DIRECTORY_CONFLICT` | `ALREADY_EXISTS`、`VENDOR_NOT_EMPTY`、`BUILTIN_PROTECTED`、`ENTITY_DELETED` |
| | `DIRECTORY_NOT_FOUND`、`RESOURCE_NOT_FOUND` | `VENDOR_NOT_FOUND`、`APPLICATION_NOT_FOUND`、`FILE_NOT_FOUND` 等具体码；无路由为 `NOT_FOUND` |
| | `INVALID_DIRECTORY` | `VALIDATION_FAILED` |
| | `DIRECTORY_UNAVAILABLE`、`LOCAL_STORAGE_UNAVAILABLE` | `STORAGE_UNAVAILABLE` |
| | `DIRECTORY_DELETE_PENDING` | `APPLICATION_DELETE_PENDING` |
| | `DOWNLOAD_CAPACITY_EXCEEDED` | `TRANSFER_CAPACITY` |
| | `PREVIEW_INVALID`、`CLEANUP_INVALID`、`REFRESH_INVALID`、`RETENTION_INVALID` | `PREVIEW_NOT_FOUND`、`PREVIEW_STALE`、`OPERATION_IN_PROGRESS`、`SOURCE_CHANGED`、`CACHE_ENTRY_NOT_FOUND`、`CHANNELS_UNVERIFIED` |
| | `RESOURCE_CONFLICT`（托管文件） | `FILE_CONFLICT`、`TRANSFER_ID_IN_USE`、`TRANSFER_CANCELLED` |
| | `PREWARM_CONFLICT` | `PREWARM_REQUEST_CONFLICT`、`OPERATION_IN_PROGRESS`、`SOURCE_CHANGED` |
| | 400 `ORIGIN_REJECTED`（Host/转发头无效） | 400 `REQUEST_ORIGIN_INVALID`；403 `ORIGIN_REJECTED` 只表示 Origin 不符 |
| 非 JSON 请求体 | 400 | 415 `UNSUPPORTED_MEDIA_TYPE` |
| `PREWARM_BUSY` 错误体 | 额外带 `job`，缺 `request_id`/`retryable` | 标准 `Error` |
| `request_id` | 只在错误时随机生成 | 每个请求生成一次，所有响应带 `X-Request-Id`，与日志一致 |
| revision | `If-Match` 或请求体 `revision`，部分接口二者都可选 | 只用 `If-Match`，所有写操作必填；请求体不含 `revision` |
| 成功响应包装 | `{vendor: …}`、`{app: …}`、`{job: …}`、`{result: …}`、`{item: …}`、`{sources: …}`、`{providers: …}` | 直接返回对象；列表统一为 `items` |
| 无内容响应 | `{ok: true}`、`{deleted: true}`、`{cancelled: true}` | `204`（应用删除仍返回 `{cleanup_pending}`） |
| 创建 | `200`（部分 `201`） | `201`，带 `ETag` 和 `Location` |
| 页码超出范围 | 回退到最后一页 | 返回空 `items` 和请求的 `page` |
| 游标错误 | 400 `INVALID_REQUEST` | 400 `INVALID_CURSOR` |
| HEAD | 只有托管、HTTP 缓存文件和部分图标 | 所有 GET 路由（ServeMux 语义）；发布制品的 HEAD 不触发下载 |

### 认证

| 现有 | 规范 |
| --- | --- |
| `POST /admin/api/login` → `{csrf}` | `POST /admin/api/session` → `201 {csrf_token, expires_at}` |
| `GET /admin/api/session` → `{csrf}` | 同路径 → `{csrf_token, expires_at}` |
| `POST /admin/api/logout` | `DELETE /admin/api/session` → `204` |
| `POST /admin/api/password` `{old, new}` → `{ok}`，失败统一 400 | `{current_password, new_password}` → `204`；`CURRENT_PASSWORD_INCORRECT`、`PASSWORD_INVALID`；错误的当前密码计入该客户端的登录限速（新增） |

### 公开 API

| 现有 | 规范 |
| --- | --- |
| `GET /api/bootstrap` 含全部公开应用 `apps`、`public_origin` | 去掉 `apps`；`public_origin` → `public_url`；`revision` 不再覆盖应用 |
| `GET /api/apps`（全部应用数组） | 删除，用 `listCatalog` |
| 公开应用 `id` 是 `vendor/app` | `key` 为 `vendor/app`，`id` 为应用 ID |
| `summary` | `description` |
| `publisher`、`origin`、`detail_url`、`distribution_url`、`channels`、`installers`（含 `shell`/`runner`/`label`）、`update_policy` | 删除（详情路径为 `/{key}`，安装命令在使用说明中） |
| `instructions`（原始 Markdown） | `instructions_available`（各语言是否非空）+ `revision`（iframe 重建键） |
| `latest_known_version` 仅在有版本能力时出现 | 始终存在，不适用或未知时为 `null` |
| `/api/home` 的 `ranking[]` 是应用加 `download_clients`；`bucket_hours` | `ranking[]` 为 `{app, download_clients}`；删除 `bucket_hours` |
| `/api/search` 条目 `id`、`url`；应用无 `localized_icons` | `key`；删除 `url`；应用的 `localized_icons` 为 `null` |
| `/api/vendors/{vendor}` 接受目录查询参数 | 不接受任何查询参数 |
| 使用说明中的 `{{install_commands}}` 旧占位符 | 删除，原样保留为文本 |

### 目录、配置与分类

| 现有 | 规范 |
| --- | --- |
| `GET /admin/api/vendors/{v}/apps`（含 `view=table`）和 `GET /admin/api/apps` | 合并为 `GET /admin/api/apps?vendor=`，总是返回表格字段，支持排序；默认 `limit` 20；`latest_version` 为空时 `null` |
| `POST /admin/api/vendors/{v}/apps` | `POST /admin/api/apps`，`vendor` 在请求体 |
| 厂商/应用 `PATCH` 可改名称、描述、图标、上游等字段 | 只接受 `{enabled}`；其余字段用配置 `PATCH` |
| `DELETE` 厂商/应用带 JSON 请求体（`revision`、`confirm_key`、`confirm_uid`） | 无请求体：`If-Match`，应用另需查询参数 `confirm_uid`；厂商删除返回 `204` |
| 重试已完成的应用删除返回 `200 {deleted: true}` | 返回 `404 APPLICATION_NOT_FOUND`，客户端视为完成 |
| `runtime_revision` | 删除 |
| 应用总是含 `base_url`、`base_urls`、`source_strategy`、`cache_ttl_seconds` | 按 Provider：发布类只有 `base_url`；HTTP 缓存只有 `base_urls`、`source_strategy`；info/hosted 都没有。配置覆盖和来源列表同样处理 |
| `GET/PUT /admin/api/apps/{v}/{a}/settings`（`channel_ttl_seconds`） | 删除，用配置路径 `cache_ttl_seconds` |
| `GET/PUT /admin/api/apps/{v}/{a}/instructions`（独立 revision，`entity_revision`） | 删除，用配置路径 `instructions.en`/`instructions.zh-CN`；`Configuration.instructions_revision` 删除 |
| `GET/PUT /admin/api/apps/{v}/{a}/cache/policy` | 删除，用配置路径 `http_policy.*` |
| `GET /admin/api/providers` → `{providers}`，空默认值为 `""`/`0` | `{items}`，空默认值为 `null` |
| 分类 `PATCH` 用请求体 `revision`；条目含 `kind: "categories"` | `If-Match`；删除 `kind`；新增 `GET /admin/api/categories/{category}` |
| 分类可以改名为其他分类已用的名称 | 改名后的中英文名称（不区分大小写）不得与其他分类重复，否则 `VALIDATION_FAILED` |
| 管理备注首次保存前 revision 为 `0`，请求体可带 `revision` | 首次保存前为 `1`，只用 `If-Match` |
| 没有应用的内置厂商可以删除 | `409 BUILTIN_PROTECTED` |
| 重试未完成的应用删除须带最初的 revision | 不再比较 revision（`If-Match` 仍须存在），只按 `confirm_uid` 定位 |
| 配置 `PATCH` 可以设置不适用于 Provider 的路径（如 Codex 的 `base_urls`） | `400 VALIDATION_FAILED` |
| 首页设置 `keys` | `pinned_app_keys` |
| `POST /admin/api/assets/icons` | `POST /admin/api/icons` |
| `GET /admin/api/assets/builtin-icon?path=` | 删除；图标字段总是站点绝对路径，直接用 `/assets/icons/...` 或 `/assets/presets/...` |

### 导入导出与复制

| 现有 | 规范 |
| --- | --- |
| 预览响应 `{id, digest, expires_at, preview: {items, ready, needs_instructions_trust}}` | 拍平为一个对象 |
| 条目和选择的 `kind`：`Vendor`、`App`、`categories` | `vendor`、`app`、`category`（导出 `selection.kind` 同样） |
| 执行请求 `{confirm: true, trust_instructions}` | `{trust_instructions}` |
| 复制请求体 `source_revision` | `If-Match`；响应为应用对象 |
| 导出厂商时缺省包含其全部应用 | `include_apps` 缺省为 `false`，需显式请求；应用选择不接受该字段 |
| 预览不存在、过期、属于其他会话或提交时会话已变 → `409`；回执过期 → `409` | `404 PREVIEW_NOT_FOUND` |
| 预览未就绪或缺少信任确认 → `400` | `409 IMPORT_NOT_READY`、`400 INSTRUCTIONS_TRUST_REQUIRED` |
| 预览后对象被修改 → 通用 `409` | `409 PREVIEW_STALE` |

### 概览与事件

| 现有 | 规范 |
| --- | --- |
| `GET /admin/api/status` 含 `name`、`os`、`arch`、`go`、`goroutines`、`memory_bytes`、`counters`、`disk`、`rates`、`public_base_url`、`application_version_counts`、`started` | 只保留 `sampled_at`、`started_at`、`metrics` |
| 应用状态含 `application`、`version_count`、`counters`、`public_base_url` | 只保留 `sampled_at`、`metrics`；info/hosted 返回 `404 CAPABILITY_UNSUPPORTED` |
| `GET /admin/api/history?scope=global&…` | 删除 `scope` |
| 历史 `time`/`from`/`to` 为 Unix 秒，`app_id` 为内部 `app/<uid>`，有 `retired` | RFC 3339；`app_key`；删除 `retired` |
| 指标 `group` 为英文标题 | 枚举 `disk`、`traffic`、`speed`、`runtime`、`resources` |
| 已退役指标 `counters.reuse_requests`、`events.recent_total` 仍可查询 | 从目录删除 |
| 事件 `resource`（与 `resource_key` 重复）、`app_id`、空字符串 | 删除 `resource`；`app_key`；空值为 `null` |
| `GET /admin/api/apps/{v}/{a}/events` | 删除（界面不可达） |

### 发布类应用（Codex、Claude Code）

| 现有 | 规范 |
| --- | --- |
| 版本、资源列表同时支持 `page` 和 `cursor` | 只用游标；版本总数见应用指标 `versions.total` |
| 版本按字符串升序 | 按 Provider 的版本顺序从新到旧，无法解析的版本按字符串排在最后；游标绑定 source epoch |
| 游标用于其他列表、应用、epoch 或过滤条件时 400 `INVALID_REQUEST` | 400 `INVALID_CURSOR`；`version` 过滤不是规范版本时 400 `INVALID_QUERY` |
| 版本条目 `bytes` | `downstream_bytes` |
| 资源条目为 PascalCase，含本地文件路径 `Path`、`Resource.{Application,MetricsID,SourceFence,Labels,Source}` 等内部字段 | `Resource` schema，snake_case，删除内部字段 |
| `POST .../cleanup/preview?source_epoch=`，响应 `{job: {ID, Selected, …}, logical_bytes, reclaimable_blob_bytes, active, unknown_versions}` | `POST .../version-cleanup/preview`，`source_epoch` 在请求体，`201 VersionCleanupPreview`（`reclaimable_bytes`、`active_generations`） |
| `POST .../cleanup/{id}/execute?source_epoch=` → `{ok}` | `POST .../version-cleanup/{id}/execute` → 已执行的预览；不再需要 `source_epoch`；重复执行返回同一结果 |
| 预览只返回一次估算 | 预览持久化 `reclaimable_bytes`、`active_generations`、`unknown_versions`，执行结果中仍可读（schema 13） |
| 预览、执行失败都是 409 `CLEANUP_INVALID` | 未知或过期 404 `PREVIEW_NOT_FOUND`；来源变化 409 `PREVIEW_STALE`；预览期间来源变化 409 `SOURCE_CHANGED`；不存在的 `source_epoch` 404 `SOURCE_NOT_FOUND`；已删除应用 409 `ENTITY_DELETED` |
| 保留预览请求体 `{revision}` | `If-Match`；`201`（带 `Location`）；预览生成期间配置被修改也返回 409 `REVISION_CONFLICT` |
| 保留失败都是 409 `RETENTION_INVALID` | 渠道无法验证 502 `CHANNELS_UNVERIFIED`；读取并发已满 503 `TRANSFER_CAPACITY`；应用或厂商已禁用（预览和执行）409 `APPLICATION_DISABLED`；预览期间来源变化 409 `SOURCE_CHANGED`；执行时策略、来源或渠道变化 409 `PREVIEW_STALE`；未知或过期 404 `PREVIEW_NOT_FOUND` |
| 保留预览 `expires` | `created_at`、`expires_at`、`executed_at`、`result` |
| 保留条目响应带 `result` | 新增 `GET .../retention/{id}` 读取预览和回执；条目响应只是分页 |
| 保留执行返回回执 | 返回带 `result` 的预览；已执行的预览直接返回已有回执，不再改写保留状态 |
| 保留状态 `{}` 或 `{attempt, success, outcome, reason, …, next_check}` | `{last_run, next_check_at}`，`last_run` 为 `{attempted_at, succeeded_at, …}` 或 `null` |
| 来源列表 `{sources}` | `{items}` |

### HTTP 缓存

| 现有 | 规范 |
| --- | --- |
| `GET .../cache` 一次返回全部条目 | `GET .../cache/entries`，游标分页；`etag` 为空时 `null` |
| `POST .../cache/match` → `{matches, canonical_path}` | `POST /admin/api/path-match` → `{matches, path}`（与应用无关） |
| `GET .../cache/cleanup/status`（按应用路径返回全局状态） | `GET /admin/api/cache/auto-cleanup` |
| 清理/刷新后续请求需重复 `source_epoch` | 只在创建清理预览时（请求体）指定；预览记录 `source_epoch`，后续按 ID 定位 |
| 预览条目 `next_cursor` 末页为 `""`；含 `access_bucket`、`basis`、`before`、`match`、`rule_index` | `null`；删除这些字段 |
| 清理执行返回 `{result}`；刷新执行 `200` | 都返回预览对象（`result` 带 `kind` 区分）；刷新执行 `202` |

### 预热

| 现有 | 规范 |
| --- | --- |
| `POST .../prewarm/start` | `POST .../prewarm/jobs`，新任务 `201`，重复 `request_id` 返回已有任务 `200` |
| `.../prewarm/{id}`、`/items`、`/cancel`、`/retry` | `.../prewarm/jobs/{id}/…` |
| 取消返回 `{cancel_requested: true}` | `202` 返回任务 |
| 选项 `release`（布尔）、`limits` | `kind`（`release`/`http_cache`）、`default_limits` |
| 任务 `created`/`updated`，PascalCase `AppRevision`/`VendorRevision`，可省略的 `reason`/`target`/`platforms` | `created_at`/`updated_at`，删除内部 revision，字段总是存在（可为 `null`/`[]`） |
| 输入错误 400 `INVALID_REQUEST`，其他失败都是 409 `PREWARM_CONFLICT` | 字段值无效 400 `VALIDATION_FAILED`；`request_id` 冲突或过期 409 `PREWARM_REQUEST_CONFLICT`；重试运行中的任务 409 `OPERATION_IN_PROGRESS`；应用或厂商已禁用 409 `APPLICATION_DISABLED`；来源变化 409 `SOURCE_CHANGED` |

### 托管文件

| 现有 | 规范 |
| --- | --- |
| 删除文件 `{deleted: true}`，取消传输 `{cancelled: true}` | `204` |
| 传输进度 `total: -1` 表示未知；可能看到 `complete` | `total_bytes: null`；状态只有 `receiving`、`committing` |
| multipart 中 `path`、`expected_id` 顺序任意 | 固定为 `path`、可选 `expected_id`、`file`，否则 400 `INVALID_REQUEST`；路径或 ID 无效 400 `VALIDATION_FAILED` |
| 冲突都是 409 `RESOURCE_CONFLICT`；导入 URL 无效与下载失败都是 502 | `FILE_CONFLICT`、`TRANSFER_ID_IN_USE`、`TRANSFER_CANCELLED`，传输期间应用被修改 409 `SOURCE_CHANGED`；URL 无效（凭据、片段、非 http(s)）400 `VALIDATION_FAILED`，下载失败 502 `IMPORT_SOURCE_FAILED`；导入 URL 可带查询串（签名链接） |

### 分发与静态资源

| 现有 | 规范 |
| --- | --- |
| 旧图标路由 `/assets/builtin/*`、`/{vendor}/{app}/icon.svg` | 删除；预置图片只在 `/assets/presets/...` |
| 只读缓存 `only-if-cached` 未命中时由缓存模块写出非 JSON 的 504 | `504 CACHE_MISS`（`Error`） |
| 上游失败和元数据校验失败都是 `METADATA_UNTRUSTED` | 网络/超时/5xx 为可重试的 `UPSTREAM_UNAVAILABLE`，校验失败为 `METADATA_UNTRUSTED` |
| 未知或未发布的 `/{vendor}`、`/{vendor}/{app}` 页面返回 `404` JSON | `index.html` 并带 404 |
| 后台页面与公开页面共用 `index.html` | `admin.html`（含 404 文档），不回退到 `index.html` |

## 校验规范

```sh
uv run --no-project --with openapi-spec-validator python -m openapi_spec_validator api/openapi.yaml
```

同样的结构检查（`$ref` 可解析、operationId 唯一且为 camelCase、标签已定义、路径参数一致、错误码在目录中且对应状态已声明、属性为 snake_case、schema 可编译）由 `go test ./internal/httpserver -run TestSpecStructure` 执行，属于门禁。
