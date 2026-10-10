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
- **预览**：清理、保留与刷新先建冻结预览，执行只作用于冻结集合，预览 ID 是幂等键，再次执行返回同一回执。未知、其他应用或已过期的预览为 404 `PREVIEW_NOT_FOUND`，来源 fence 或策略变化为 409 `PREVIEW_STALE`（构建时为 `SOURCE_CHANGED`），构建或执行中为 409 `OPERATION_IN_PROGRESS`；机制见 [architecture.md](architecture.md#冻结预览)。
- **禁用的应用**：需要已启用应用的动作（保留预览与执行、预热启动与重试、HTTP 缓存刷新）在应用或其厂商被禁用时返回 409 `APPLICATION_DISABLED`（不可重试），不用可重试的 `SOURCE_CHANGED`；`SOURCE_CHANGED` 只表示来源在请求处理期间发生了变化。只读和管理已保留数据的操作（列表、状态、清理）对禁用的应用照常可用。
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

路由层（尚未选中操作）只会返回 `x-router-error-codes`：`NOT_FOUND`、`METHOD_NOT_ALLOWED`（带 `Allow`）、`INVALID_PATH`、`REQUEST_ORIGIN_INVALID`。路径不存在时对任何方法都是 `404 NOT_FOUND`；路径存在但方法不对时是 `405 METHOD_NOT_ALLOWED`。

## 服务端实现

实现结构（构造、路由表、中间件顺序、错误与请求辅助、测试工厂、契约校验）见 [architecture.md](architecture.md#http-层)。

- 每个操作在 `internal/httpserver/routes.go` 的路由表中占一行，用 `http.ServeMux` 的方法 + 路径模式注册；`x-greedy` 参数用 `{name...}`，`/` 用 `/{$}`。GET 模式同时应答 HEAD，所以规范中的 HEAD 操作和 `install.sh`/`install.ps1` 不单独注册（路由表用 `servedBy` 标明由哪个操作的注册应答）；安装脚本若单独注册，会与 `/api/vendors/{vendor}`、`/admin/vendors/{vendor}` 等三段路径互相冲突。
- `/admin/{ui_path}` 与 `/{vendor}/{app}` 系列在 ServeMux 中互不包含，不能直接注册：按 `x-spa-routes` 逐条注册后台页面；其余 `/admin/...` 落到 `/{vendor}/...` 处理器，由它识别保留厂商名后返回后台文档并带 404。其他保留厂商名（`api`、`assets`、`health`、`all`）不进入应用查找。
- `/{vendor}/{app}/{file_path}` 的 GET 模式匹配任意 GET 路径，ServeMux 自己的 404/405 判断因此不适用于保留路径：`/admin/api/`、`/api/`、`/assets/`、`/health/` 下的路径若有操作以其他方法注册则返回 `405`（`Allow` 只列这些方法），否则对任何方法都返回 `404 NOT_FOUND`（`spa.go` 的 `reservedRouteError`）。
- 页面文档按 `x-spa-routes.documents`：公开页面（`/`、`/all`、`/{vendor}`、`/{vendor}/{app}`）返回 `index.html`；`/admin/...` 全部返回 `admin.html`，从不回退到 `index.html`。前端构建缺少某个入口时该类页面返回 `500 INTERNAL_ERROR`。文件都在构建目录根部（`index.html`、`admin.html`、`assets/`），布局只在 `spa.go` 的常量中定义。
- 厂商或应用不存在、未发布或标识无效时，`/{vendor}` 和 `/{vendor}/{app}` 返回 `index.html` 并带 404，由 SPA 显示“页面不存在”；其下的分发路径仍返回规范中的 JSON 错误。保留路径的 404/405 见上一条。
- 查询参数、请求体上限和鉴权按路由声明，由中间件执行；`TestRouteTableMatchesSpec` 保证与规范一致。JSON 严格解码（`jsoncheck.Strict`）、`If-Match` 解析和错误码都是共享辅助函数。
- 错误码常量在 `error_codes.go`，与 `components.x-error-codes` 由测试保持一致；状态和 `retryable` 只来自这张表。
- 契约测试：`newHarness` 的每个响应都按规范校验（状态、`X-Request-Id`、安全头、声明的响应头、媒体类型、JSON Schema 2020-12 响应体、错误码归属与目录一致）；`spec_test.go` 检查路由表、`x-spa-routes`、错误码目录和规范自身的结构。

## 前端

- 用 `openapi-typescript` 从规范生成类型，用 `openapi-fetch` 作为客户端；不手写 DTO。生成物（`frontend/src/shared/api/*.gen.ts`）与规范一起提交，`npm run codegen:check` 检查可复现。
- **SPA 文档**：前端有两个入口。`x-spa-routes.routes` 中的公开路径（`/`、`/all`、`/{vendor}`、`/{vendor}/{app}`）返回嵌入目录根部的 `index.html`；其中的 `/admin/...` 路径返回 `admin.html`，其余 `/admin/...`（`/admin/api/` 除外）返回 `admin.html` 且状态为 404。`/admin/...` 不能回退到 `index.html`。文件名写在 `x-spa-routes.documents`，CSP 与静态资源规则见 [frontend.md](frontend.md#两个入口与-spa-服务契约)。前端测试要求前端的具名路由与 `x-spa-routes.routes` 完全一致。
- TanStack Query 的 key 以 operationId 加参数构成；写操作成功后用响应体替换缓存（响应总是完整的新状态和新 `ETag`）。
- 409 `REVISION_CONFLICT` 保留草稿并提示重新加载；`retryable: true` 的错误可提供重试。
- MSW 的模拟响应使用生成的类型，示例可直接取自规范。

## 校验规范

```sh
uv run --no-project --with openapi-spec-validator python -m openapi_spec_validator api/openapi.yaml
```

同样的结构检查（`$ref` 可解析、operationId 唯一且为 camelCase、标签已定义、路径参数一致、错误码在目录中且对应状态已声明、属性为 snake_case、schema 可编译）由 `go test ./internal/httpserver -run TestSpecStructure` 执行，属于门禁。
