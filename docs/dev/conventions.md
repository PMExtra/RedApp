# 编码约定

本文是 [AGENTS.md](../../AGENTS.md) 中规则的细化。标注 **【目标】** 的条目描述重构完成后的状态，括号内注明落地阶段；现有代码尚未完全满足，新代码应直接按目标写，修改旧代码时顺手对齐，但不要为此扩大 PR 范围。

## Go

### 包边界

- 包按职责划分，依赖只能指向更底层的包（见 [architecture.md](architecture.md#包与依赖方向)）。叶子包（`identity`、`jsoncheck`、`pathmatch`、`media` 等）不引入其他内部包。
- `internal/httpserver` 只做 HTTP 协议转换：解析请求、调用领域服务、写响应。业务规则放在领域包里。
- 所有 SQL 都在 `internal/store` 中。其他包不能拿 `*sql.DB` 直接写 SQL。**【目标】** store 不再对外暴露底层 DB 句柄（阶段 5）。
- `internal/testutil` 等测试辅助只能被 `_test.go` 引用。

### 错误

- 错误消息用小写开头、不带句末标点，描述“做什么失败了”：`fmt.Errorf("open state database: %w", err)`。**【目标】** 现有不少错误以大写开头，因为它们曾被直接返回给客户端；HTTP 层已改为显式错误码，错误文本只进日志，修改相关代码时统一为小写（阶段 5）。
- 包装底层错误一律用 `%w`，保留错误链。
- 调用方需要区分的错误，定义 sentinel（`var ErrConflict = errors.New(...)`）或带字段的类型化错误，用 `errors.Is` / `errors.As` 判断。
- **禁止按错误文本分类**，例如 `strings.Contains(err.Error(), "SHA256")` 或比较已持久化的错误字符串。需要持久化错误类别时，单独存一个稳定的代码字段。
- 内部错误文本不直接返回给 HTTP 客户端；对外只给稳定的错误码和面向用户的消息（见下文 HTTP API）。

### 并发

- 持有 mutex 时不做 I/O、哈希计算（含 bcrypt）或数据库调用。先在锁内拷贝需要的状态，解锁后做慢操作，再加锁提交结果并检查期间是否被并发修改。
- 确实需要在锁内做 I/O 的（例如为保证提交顺序），必须在锁的声明处注释说明原因和持锁的最长操作。
- 每个 goroutine 都要有明确的退出条件（context 取消或 channel 关闭），并在关闭流程中等待其结束。

### context

- `context.Context` 是第一个参数，命名为 `ctx`，不存进结构体。
- 不用 context 传可选参数或业务数据；只用于取消、截止时间和请求范围的元数据（如 request_id）。

### 可测试性

- 生产结构体里不放测试钩子字段（如 `testFault`、`testConfigurationPrepare`）。需要注入时钟、故障或外部依赖，用构造函数选项或小接口，由测试传入实现。

### 数据库

- SQL 只写在 `internal/store`。
- `INSERT` 显式列出列名，不依赖表的列顺序。
- 外键声明 `ON DELETE CASCADE`（或明确说明为什么不级联）；连接上启用外键约束。
- 多步写入放在一个事务里；配置类写入用 revision 做乐观并发控制，冲突返回 sentinel 错误，由 HTTP 层转换成 409。
- schema 变化遵守 [ADR 0001](adr/0001-pre-1.0-no-migrations.md)：1.0 前提升 schema 版本并拒绝旧目录，不写迁移。
- **【目标】** 每个实体独立 CAS，不再用整份配置快照的单一 revision（阶段 5）。

### 文件

- 原子写入：同目录临时文件 → 写入 → `fsync` 文件 → `rename` → `fsync` 目录。不要在各包中重复实现这一流程。
- 原子写、目录 fsync、暂存后 rename 发布、只读打开存储文件、删除和随机 ID 使用 `internal/fsutil`，不要在包内另写一份。读取已发布的文件用只读打开（`fsutil.OpenRegular`），只有原地续写的文件才以读写方式打开。
- 数据目录权限为 `0700`，文件为 `0600`。

### 日志

- HTTP 层使用 `log/slog` 结构化日志：每个请求一行访问日志，错误日志带错误码和底层错误，都带 `request_id`，与 `X-Request-Id` 和错误响应中的 `request_id` 一致。`internal/` 其他包通过回调上报错误，由调用方记录。
- 不记录密码、会话 token、CSRF token、代理凭据或完整的带凭据 URL。

### 注释与格式

- 注释解释“为什么”：约束来源、非显然的取舍、安全理由。不复述代码在做什么，不写版本号或变更历史（这些属于 git 历史）。
- `gofmt` 覆盖 `cmd internal installers presets`，由 `make check` 检查。

## HTTP API

全部路由以 [`api/openapi.yaml`](../../api/openapi.yaml) 为准，改接口先改规范（[ADR 0009](adr/0009-openapi-contract.md)）；组织方式、错误码目录用法和与旧实现的差异见 [api.md](api.md)。

### 路径与方法

- 公开接口在 `/api/`，管理接口在 `/admin/api/`。分发路径是 `/<vendor>/<app>/<file_path>`，不加 `/api` 前缀。
- 资源用名词复数路径，应用资源固定以 `/apps/<vendor>/<app>` 定位，不用请求头选择应用。
- GET 只读、可重试；PUT 整体替换设置；PATCH 用于稀疏修改（配置用 `set`/`unset`）；POST 用于创建或动作（如 `.../cleanup/preview`）；DELETE 删除。
- 创建返回 `201`，没有响应体的成功返回 `204`；成功响应直接返回资源对象，不再包一层 `{"app": ...}`。
- 查询参数白名单校验，未知或重复的参数返回 400。
- 路由用标准库 `http.ServeMux` 的方法 + 路径模式注册（`internal/httpserver/routes.go` 的路由表，每个规范操作一行）；鉴权、CSRF、Origin 检查、request_id、日志、查询白名单和请求体上限是中间件（见 [architecture.md](architecture.md#http-层)）。

### 请求与响应体

- JSON 字段名用 `snake_case`。
- 请求体必须是 `application/json`（否则 415），限制大小，拒绝重复键、未知字段和尾随数据、无效 UTF-8 和 `null`（`decodeJSON`，基于 `jsoncheck.Strict`；接受 `null` 的请求用 `decodeJSONNullable`）。
- 时间用 RFC 3339 UTC 字符串；字节数、计数用整数。
- 不返回内部字段（本地文件路径、存储命名空间、内部 revision），不保留重复或兼容字段。

### 错误响应

错误响应体：

```json
{"error": {"code": "REVISION_CONFLICT", "message": "...", "request_id": "...", "retryable": false}}
```

- `code` 是稳定的大写蛇形标识，前端按 `code` 而不是 HTTP 状态或消息文本做判断。错误码、状态和 `retryable` 由规范中的错误码目录（`components.x-error-codes`）定义，新增错误码先加入目录。
- 每个错误场景显式指定 `code`（`s.fail(w, r, code, cause, message)` 或 `newError`），不由 HTTP 状态推导；底层错误只进日志。
- `request_id` 在请求入口生成一次，写入 `X-Request-Id` 响应头、错误响应和日志。
- `message` 面向用户，不包含内部错误文本、路径或 SQL。

### 并发控制（revision）

- 可编辑资源的 GET 返回 `revision` 字段和 `ETag: "<revision>"`。
- 所有修改请求带 `If-Match: "<revision>"`，请求体不携带 `revision`。缺少或格式错误返回 `400 IF_MATCH_REQUIRED`，不匹配返回 `409 REVISION_CONFLICT`，前端保留用户草稿。
- 防止误操作同名重建对象的 UID 守卫（如删除应用的 `confirm_uid`）不匹配时同样返回 `409 REVISION_CONFLICT`。
- 不可变对象（按 ID 寻址的托管文件）和绑定冻结状态的预览/任务动作不需要 `If-Match`。
- 成功响应返回新的 revision 和完整的新状态，前端用它替换基线。

### 分页

- 时间序或无限增长的列表（版本、资源、事件、缓存条目）用游标分页：`?limit=&cursor=`，响应 `{"items": [...], "next_cursor": "..." | null}`。默认值按接口而定（通常 50），最大 100。游标对客户端不透明，用在其他接口或过滤条件上返回 `400 INVALID_CURSOR`。
- 需要页码导航的有限列表（厂商、应用、分类、托管文件）用 `?page=&limit=`，响应 `items`、`page`、`limit`、`total`、`total_pages`，默认值按页面而定，最大 100。页码超出范围返回空 `items`。
- 先过滤、再分页；总数不随页码变化。

## 前端

前端按 [ADR 0008](adr/0008-frontend-stack.md) 构建，架构与扩展方式见 [frontend.md](frontend.md)。

### 目录结构

```text
frontend/src/
  app/              # 入口、路由、全局 provider；public 与 admin 各一个入口
  shared/           # 跨领域复用：api、ui、forms、i18n、lib、styles
  features/<domain>/  # 领域功能：查询/变更 hooks、表单 schema、领域组件、文本
  pages/            # 路由页面，只组合 features，不直接请求 API
```

- `features` 之间只通过 `index.ts` 引用；需要共享的提升到 `shared`。`shared` 不引用 `features`/`pages`/`app`。这些由 ESLint 检查。
- API 类型由 OpenAPI 规范生成，不手写重复的 DTO 类型。
- 服务端数据用 TanStack Query 管理，不复制到 Pinia；Pinia 只放纯客户端状态（会话、界面偏好）。
- 可编辑资源的写操作用 `useRevisionedMutation`（`If-Match`）；409 冲突保留草稿并提示重新加载。
- 表单用 vee-validate + zod（`@/shared/forms`）；离开未保存的页面要确认，确认框不用 `window.confirm`。
- 样式只用设计令牌对应的 Tailwind 工具类，不写颜色字面量、内联 `<style>` 或静态 `style` 属性（SPA 的 CSP 禁止）。

### 国际化

- 所有用户可见文本走 vue-i18n，不在组件里硬编码；key 是语义化路径，不用英文原文当 key。
- 文本放在所属模块的 `locales/en.ts` 与 `locales/zh-CN.ts` 中；中英文 key 集合与占位符必须完全一致，由测试检查；缺 key 视为失败。
- 错误按 `code` 显示 `errors.codes.*` 的本地化文本；规范新增错误码时同时补译文。
- 新增路由必须同时加入规范的 `x-spa-routes` 和服务端 `internal/httpserver/spa.go` 的 `adminSPARoutes`（`spaRoutes.test.ts` 与 `TestSPARoutesMatchSpec` 检查）。

### 前端测试

- 组件测试用 Testing Library 按角色和可见文本查询，不依赖组件内部状态或 CSS 类名。
- 网络用 MSW 模拟（`mockApi`、`apiError`），响应形状来自生成的 API 类型，夹具来自 `src/test/factories`。
- Playwright 冒烟测试覆盖登录、主要导航和一次完整的保存流程，在嵌入了前端的真实 Go 服务上运行。

## 测试

- **按行为命名测试文件**：`revision_conflict_test.go`、`cache_cleanup_test.go`。不要用版本或过程命名，例如 `v072_test.go`、`review_test.go`、`upgrade_v071_test.go`。
- 测试函数名描述行为：`TestImportRejectsDuplicateApplication`。
- 不用 `time.Sleep` 做同步。用 channel、`sync.WaitGroup`、可注入时钟或轮询加超时等待明确的条件。
- 测试数据通过共享工厂函数构造（**【目标】** 每个包一个 `_test.go` 中的工厂，或跨包的测试辅助，阶段 2），不要在每个测试里手写整份配置。HTTP 测试统一用 `internal/httpserver/harness_test.go` 的 `newHarness`，它还按规范校验每个响应。
- 断言可观察行为（HTTP 响应、持久化结果、公开 API 返回值），不断言私有字段、调用次数或实现细节。
- 不写同义反复的测试：例如把常量和自身比较、只验证 mock 返回了 mock 设定的值。
- 表驱动测试覆盖同一行为的多个输入；不要为每种参数组合复制一份测试。
- 失败恢复路径（中断、超时、磁盘错误、并发冲突）与正常路径同等重要。

## 脚本

- Python 只用标准库。
- 生产脚本（CI、发布、安装器维护）用显式检查并抛出带说明的异常或 `sys.exit(<消息>)`，不要用 `assert` 做校验（`python -O` 会移除 `assert`）。测试脚本可以用 `assert` 或 `unittest` 断言。
- 共享逻辑放在支持模块中（如 `installer_test_support.py`、`installer_manifest.py`），不要在脚本之间复制粘贴。启动真实 `bin/redapp` 的测试一律使用 `cli_test_support.py`，不要自行选端口、轮询健康检查或拼装登录流程。
- 格式可读：一行一条语句，不用分号拼接多条语句，不写超长单行表达式；函数有简短 docstring 说明目的。
- Shell 脚本以 `set -eu` 开头，变量加引号。

## 文档

- 用户文档（`README.md`、`docs/guide/*`）中英成对：`x.md` 与 `x.zh-CN.md` 必须在同一 PR 中修改，标题层级、代码块和表格保持一致。`make docs-check` 检查结构和链接，PR 上的 CI 还检查两种语言是否同时修改。
- 开发者文档（`AGENTS.md`、`docs/dev/`）只用中文。
- 不带版本号的文档只描述当前行为，不写“v0.x 新增”“从某版本起”之类的历史；历史看 git log。
- 不新增过程性文档（验收记录、验证日志、测试契约、版本规格、路线图）。验证证据写在 PR 描述里。
- 有长期影响的决定写 ADR：`docs/dev/adr/NNNN-<主题>.md`，包含“背景 / 决定 / 后果”，不超过 30 行。决定被推翻时新增 ADR 并在旧 ADR 顶部注明被取代。
- 写法：短段落、列表和表格；一段只讲一件事；避免把多个条件塞进一个长句。
