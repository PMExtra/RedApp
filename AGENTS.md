# 项目协作规则

细则见 [docs/dev/conventions.md](docs/dev/conventions.md)，架构见 [docs/dev/architecture.md](docs/dev/architecture.md)，HTTP 契约见 [docs/dev/api.md](docs/dev/api.md)，构建与发布见 [docs/dev/development.md](docs/dev/development.md)。

## 协作原则

- **决策**：主动提出工程建议并说明影响；未经确认不偏离用户的明确决定，以用户最新决定为准。安全或权限冲突必须明确报告。
- **范围**：先核对目标与边界，保留无关的已有改动。扩大范围、改变架构或新增需求前，说明必要性、代价和对现有授权的影响，确认后再做。
- **共性修复**：同类缺陷要检查所有同类入口，优先修共享组件，并删除被替代的旧代码；不做无关重写。
- **验证**：按真实环境和回归风险验证关键行为与失败恢复；说明验证范围和局限，不以测试数量衡量质量。
- **交付**：如实说明实现、验证、推送、发布各自的状态，给出证据和剩余阻塞；需要新授权时说明原因。

## 命令

| 命令 | 用途 |
| --- | --- |
| `make check` | 文档检查、`gofmt`、`go vet` |
| `make test` | 脚本单测、`go test -race ./...`、Shell 安装器与维护回归 |
| `make frontend-test` | 前端生成物检查（`codegen:check`）、ESLint、Prettier、类型检查与 Vitest |
| `make frontend` | 构建前端到 `internal/httpserver/web` |
| `cd frontend && npm run codegen` | 由 `api/openapi.yaml` 重新生成前端 API 类型 |
| `cd frontend && npm run format` | Prettier 格式化前端代码 |
| `make build` / `make binary` | 前端 + 二进制 / 只编译二进制 |
| `make runtime-test` | 用现有 `bin/redapp` 跑真实进程的 CLI/HTTP 集成测试 |
| `make docs-check` | 双语文档结构与 Markdown 链接 |

提交前至少运行与改动相关的门禁；改 Go 代码必须通过 `make check test`，改前端必须通过 `make frontend-test` 并重新构建；改 `api/openapi.yaml` 必须重新生成前端类型。

## 目录职责

| 路径 | 职责 |
| --- | --- |
| `cmd/redapp` | 服务入口：配置、依赖组装、后台循环 |
| `cmd/preset-inventory` | 导出预置清单 JSON 给安装器维护脚本 |
| `api/` | HTTP 契约 `openapi.yaml`（OpenAPI 3.1），服务端与前端的唯一接口来源 |
| `internal/httpserver` | HTTP 路由、鉴权、错误响应、SPA 与分发路径；`web/` 是前端构建产物 |
| `internal/apps/builtin` | 编译期发布协议与数据库应用组合成运行时注册表 |
| `internal/apps/codex`、`internal/apps/claude` | Codex / Claude Code 发布协议、平台与签名规则 |
| `internal/application` | Provider 定义、应用快照、注册表、协议接口 |
| `internal/catalog` | 渠道/元数据缓存与制品授权 |
| `internal/download` | 发布制品下载引擎（代际、续传、校验、清理、保留） |
| `internal/httpcache` | `http-cache` 应用的 HTTP 响应缓存 |
| `internal/hosted` | 管理员上传的托管文件 |
| `internal/prewarm`、`internal/warmplan` | 预热任务与预热计划上限 |
| `internal/releasemaintenance` | 定时版本保留与自动预热 |
| `internal/store` | SQLite schema 与全部持久化；**新 SQL 只能写在这里**（存量见阶段 5） |
| `internal/auth` | 管理员密码、会话、CSRF、登录限速 |
| `internal/config` | 部署配置与公共地址 |
| `internal/configexchange` | 配置导入导出格式与校验 |
| `internal/distributor` | 上游 HTTP 客户端与代理 transport |
| `internal/networkproxy` | 代理设置类型与三级继承解析 |
| `internal/cachepolicy`、`internal/pathmatch` | HTTP 缓存规则；应用内路径匹配 |
| `internal/history`、`internal/site` | 指标历史；站点文本 |
| `internal/identity` | ID 校验、保留名、UID、存储命名空间 |
| `internal/media` | 图标校验与存储 |
| `internal/jsoncheck`、`internal/yamlconfig` | 严格 JSON 校验；严格 YAML → JSON |
| `internal/instance` | 数据目录实例锁 |
| `internal/testutil` | 测试辅助，只能被测试引用 |
| `presets/` | 内置厂商/应用/分类的 YAML 模板与图标（嵌入） |
| `installers/` | 官方安装器原文、patch、生成结果与来源记录 |
| `frontend/` | Vue 前端源码：`index.html`（公开）与 `admin.html`（后台）两个入口，结构见 [frontend.md](docs/dev/frontend.md) |
| `scripts/` | 构建、发布、安装器维护与集成测试脚本（Python 标准库 / sh） |
| `config/` | 部署配置示例 |
| `third_party/` | 第三方许可证原文与清单 |
| `docs/guide/` | 用户文档（中英双语） |
| `docs/dev/` | 开发者文档与 ADR（中文） |
| `.github/` | CI、发布、安装器每日检查工作流 |

## 硬性约束

- **Schema**：1.0 前不写数据迁移。任何 schema 变化都提升 `store.SchemaVersion`，旧数据目录被拒绝。1.0 起每次 schema 变化必须附带迁移和 golden fixture 测试（[ADR 0001](docs/dev/adr/0001-pre-1.0-no-migrations.md)）。
- **安装器**：禁止编辑 `installers/*/*/upstream/`。只改 `patches/`，用 `scripts/update-installers.py` 重新生成 `generated/`；patch 不允许 fuzz（[installers.md](docs/dev/installers.md)）。
- **前端产物**：`internal/httpserver/web` 是提交的构建产物。前端源码或依赖变化必须重新构建并提交，且必须能由锁定的工具链逐字节复现。
- **SPA 路由**：新增前端路由必须加入规范的 `x-spa-routes`（前端测试检查两者一致），服务端按它返回 `index.html` 或 `admin.html`，否则直接访问会 404。
- **依赖**：新增或升级 Go/npm 依赖必须同步更新 `third_party/README.md` 和对应许可证原文。
- **HTTP 契约**：路由、字段和错误码以 `api/openapi.yaml` 为准，改接口先改规范并在同一 PR 中改实现（[ADR 0009](docs/dev/adr/0009-openapi-contract.md)）。
- **并发写**：后台写操作使用 revision（`If-Match`）做 CAS，冲突返回 409，不允许静默覆盖。
- **格式**：`gofmt` 覆盖 `cmd internal installers presets`，由 `make check` 强制。
- **Provider**：Provider 在编译期定义，不引入插件或可执行配置（[ADR 0002](docs/dev/adr/0002-compile-time-providers.md)）。

## 文档

- 用户文档（`README.md`、`docs/guide/*.md`）中英成对（`x.md` + `x.zh-CN.md`），必须在同一 PR 中一起修改并保持内容一致；`make docs-check` 检查结构，PR CI 检查是否同时修改。
- 开发者文档只用中文，放在 `docs/dev/`。
- 不带版本号的文档只写当前行为，不写版本历史。
- 不新增验收记录、验证日志、测试契约、版本规格之类的过程文档；验证证据写在 PR 描述里。
- 有长期影响的决定写 ADR（`docs/dev/adr/`）。
- 本文件只记录已确认的长期约定，不固化临时会话偏好；具体操作流程写在 `docs/dev/`。

## PR 与提交

- 一个 PR 只解决一个关注点；重构与行为变更分开。
- 不做巨型提交：每个提交可独立理解、可通过门禁。提交标题用简短的英文祈使句。
- 1.0 前不为每次小改动提升 `VERSION`，只在准备发布时提升。
