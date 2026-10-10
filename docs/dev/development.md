# 开发、测试与发布

## 本地环境

| 工具 | 版本/要求 | 用途 |
| --- | --- | --- |
| Go | `go.mod` 的 `go` 指令（当前 1.27.1） | 服务端；需要 CGO 和 C 编译器（`mattn/go-sqlite3`） |
| Node.js | `frontend/.node-version`（当前 24.19.0），使用提交的 `package-lock.json` | 前端构建与测试 |
| Python 3 | 只用标准库 | 安装器维护、CLI 集成测试、发布脚本、文档检查 |
| GNU `patch` | 支持 `--fuzz=0` | 安装器 patch 应用 |
| Docker | 可选 | 镜像构建、原生发布编译、Docker 运行测试 |
| PowerShell 7 + Windows PowerShell 5.1 | 仅 Windows | PowerShell 安装器门禁 |

Windows 主机上建议在 Linux 容器或 WSL 中运行 `make` 目标；PowerShell 安装器测试则必须在 Windows 上运行。

### 工具链版本的唯一来源

| 内容 | 唯一来源 | 使用方 |
| --- | --- | --- |
| Go 版本 | `go.mod` 的 `go X.Y.Z`（不另写 `toolchain`） | 所有工作流的 `setup-go`（`go-version-file: go.mod`） |
| Node.js 版本 | `frontend/.node-version` | 所有工作流的 `setup-node`（`node-version-file`） |
| 基础镜像 | 根 `Dockerfile` 的全局 `ARG GO_IMAGE` / `ARG NODE_IMAGE`（标签 + 摘要） | Dockerfile 各阶段；`scripts/build-native-container.sh` 与 CI 发布编译缓存键通过 `scripts/dockerfile-arg.sh GO_IMAGE` 读取 |

`make check` 中的 `scripts/check-toolchain.py` 保证：镜像标签与 `go.mod`、`.node-version` 一致且按摘要固定；工作流不写死 `go-version`/`node-version`；action 固定到 commit SHA；不使用 `pull_request_target`；`docker build` 不带 `--pull`；`.github/` 下的 Dockerfile 也按摘要固定基础镜像。升级 Go 或 Node 时同时改唯一来源和对应镜像的标签与摘要。

## 构建

```sh
make build      # 先构建前端，再编译 bin/redapp
make binary     # 只编译 Go，使用当前已提交的前端产物
make docker     # 从源码构建镜像 redapp:local
```

根 `Dockerfile` 是唯一的镜像定义（需要 BuildKit），`runtime` 阶段的 LABEL、用户、数据卷、健康检查只写一次。全局 `ARG RUNTIME_FILES` 选择运行时文件的来源：

| `RUNTIME_FILES` | 构建上下文 | 用途 |
| --- | --- | --- |
| `source`（默认） | 仓库根目录（`.dockerignore` 排除文档、`.git`、`bin`、已提交的前端产物等） | `make docker`：Node 阶段重新构建前端，Go 阶段编译 |
| `prebuilt` | 含 `redapp`、`ca-certificates.crt` 和 `0700` 的 `data/` 的目录 | CI 发布镜像与 `scripts/test-docker-local.sh`：只打包原生编译好的二进制，不拉取外部镜像 |

- 所有编译都经过 `scripts/build-binary.sh`：CGO 静态链接，tags 为 `netgo,osusergo,sqlite_omit_load_extension`，通过 `-X main.version` / `-X main.revision` 注入版本。
- 宿主上的 `make binary` 使用宿主的 C/libc，产物不能作为发布制品。
- 发布用的二进制由 `scripts/build-native-container.sh` 在 Dockerfile `GO_IMAGE` 指定的固定摘要 `golang:<版本>-trixie` 镜像中、在对应原生架构上编译（`GOAMD64=v1`、`GOARM64=v8.0`），不交叉编译、不使用 QEMU 编译。原因见 [ADR 0004](adr/0004-static-cpu-baseline-build.md)。

## 前端

前端位于 `frontend/`（Vue 3 + TypeScript + Vite，两个入口 `index.html` 与 `admin.html`），构建产物嵌入 Go 二进制，运行时不需要 Node、CDN 或单独的前端服务。架构、目录规则和扩展方式见 [frontend.md](frontend.md)。

```sh
cd frontend
npm ci
npm run codegen:check   # 生成的 API 类型与 api/openapi.yaml 一致
npm run lint            # ESLint，0 警告
npm run format:check    # Prettier
npm run typecheck       # vue-tsc
npm test                # Vitest
npm run build           # 输出到 internal/httpserver/web
```

- Vite 以 `/` 为 asset base，输出到 `internal/httpserver/web`（`index.html`、`admin.html`、扁平的 `assets/`）。**该目录是提交到仓库的产物**：前端改动必须同时重新构建并提交它（见 [ADR 0007](adr/0007-committed-frontend-bundle.md)）。CI 会重新构建并要求该目录与提交完全一致（`git diff --exit-code` 与 `git status --porcelain` 都为空，新增未提交的文件同样失败）。
- 服务端必须为公开路由返回 `index.html`、为 `/admin/...` 返回 `admin.html`，规则见 [frontend.md](frontend.md#两个入口与-spa-服务契约) 和规范的 `x-spa-routes`。
- 改了 `api/openapi.yaml` 要运行 `npm run codegen` 并提交生成的 `src/shared/api/*.gen.ts`。
- `npm run build` 还检查两件事：公开入口不包含后台模块；打包进产物的每个 npm 包都记录在 [third_party/README.md](../../third_party/README.md)。
- `npm run dev` 只是本机 loopback 预览；设置 `REDAPP_DEV_BACKEND` 可把 API 代理到本机 Go 服务。真实 HTTP 行为（CSP、Cookie、深链）要用嵌入了前端的 Go 构建来测。
- 测试：Vitest + happy-dom 的 DOM 测试（Testing Library、MSW），覆盖 API 客户端、会话与 CSRF、冲突处理、翻译完整性、路由白名单与组件的可访问行为。它不证明浏览器布局、屏幕阅读器、触屏或真实 CSP 行为；这些由 Playwright 浏览器测试（`make e2e`）补充。
- 新增前端路由时，必须同步规范的 `x-spa-routes` 与服务端 `adminSPARoutes`（见 [AGENTS.md](../../AGENTS.md)）。

## 测试门禁

| 命令 | 覆盖 | 需要 |
| --- | --- | --- |
| `make check` | 文档检查（`docs-check`）、工具链与 CI 固定检查（`toolchain-check`）、`gofmt`（`cmd internal installers presets`）、`go vet` | Go、Python |
| `make test` | 发布脚本单测、文档检查与工具链检查单测、`go test -race`（`cmd`、`installers`、`internal`、`presets`）、Shell 安装器契约、安装器更新与每日维护的离线回归 | Go、Python、`patch` |
| `make frontend-test` | 生成物检查、ESLint、Prettier、`vue-tsc` 类型检查与 Vitest DOM 测试 | Node |
| `make runtime-test` | 用**当前** `bin/redapp`（不重新编译，缺失时直接失败）跑真实进程：数据目录与配置、HTTP 路由与重启、使用说明文档执行、retention、prewarm、分类/Tag、配置导入导出 | 已构建的二进制、Node（自动 `npm ci`，供 Happy DOM 使用） |
| `make e2e` | 用**当前** `bin/redapp` 和全新数据目录启动服务，从首次启动日志读取管理员密码，发布一个 `info` 应用作为公开内容，再跑 Playwright（Chromium）：公开首页、目录搜索与应用页、公开与后台 404 文档、登录—导航—退出、站点文本保存、不带标签的应用深链；控制台不能有错误（含 CSP 违规） | 已构建的二进制、Node、Playwright Chromium（`cd frontend && npx playwright install --with-deps chromium`） |
| `make docs-check` | 双语用户文档结构一致、仓库内 Markdown 相对链接有效 | Python |
| `sh scripts/test-docker-local.sh` | 镜像配置发现、默认 serve、健康检查、数据卷；未设 `REDAPP_TEST_IMAGE` 时用根 Dockerfile 的 `prebuilt` 方式打包当前 `bin/redapp` | Docker（BuildKit）、Linux 二进制或镜像 |
| `python3 scripts/test-cpu-baseline.py` | 从实际镜像取出 amd64 二进制，检查只声明 x86-64 baseline，并在无 AVX 的 QEMU CPU 上启动 | `qemu-user`、`binutils` |
| `python scripts/test-installers.py --platform windows` | PS7 与 5.1 解析并执行全部 PowerShell 安装器 | Windows |
| `make network-test` | 联网：官方 Claude 签名清单与一个真实二进制（超过 200 MB，最长 30 分钟，2 分钟无进度即失败） | 已构建的二进制、外网；不在强制门禁中 |

### 真实进程测试

`scripts/test-*-cli.py` 都基于 `scripts/cli_test_support.py`（标准库 `unittest`），每个文件可单独运行，例如 `python3 scripts/test-http-cli.py`，也可以加 `TestClass.test_name` 只跑一个用例。

- `ServerTestCase` 为每个用例提供私有临时目录；`start_server()` 启动的服务和 `start_fixture()` 启动的本地上游在用例结束时一定被停止和清理。
- `RedAppServer` 每次启动都探测新的回环端口；探测与服务绑定之间被其他进程抢占时，服务以 `address already in use` 退出，harness 换端口重试。重启后 `base_url` 会变化。
- `Session` 模拟浏览器：Cookie、`Origin`、CSRF。`fetch()`/`request()` 默认断言 HTTP 200，其他状态用 `expect=` 显式声明，失败消息包含方法、路径、状态和响应体摘要。
- 用例失败时打印该用例所有服务的日志，初始管理员密码会被遮盖。

`docs-check` 与 `--base`：

```sh
python3 scripts/check-docs.py                 # 结构与链接
python3 scripts/check-docs.py --base main     # 另外要求成对文档同时修改
```

## CI

`.github/workflows/ci.yml` 在 PR 和 main push 上运行：

| Job | 内容 |
| --- | --- |
| `frontend` | `make frontend-test`，重新构建并比对已提交的 `internal/httpserver/web`，上传 `frontend-<SHA>` 产物（含 SHA256SUMS） |
| `test`（amd64、arm64） | `make check test`；PR 上另跑 `check-docs.py --base HEAD^1`；amd64 在无网络、只读的容器里再跑一次 Shell 安装器测试 |
| `runtime`（amd64、arm64） | 校验并解包本次 `frontend` 产物，用原生容器编译，`make runtime-test`，用根 Dockerfile（`--target runtime`、`RUNTIME_FILES=prebuilt`）打包 scratch 运行镜像并跑 Docker 测试；amd64 另跑 CPU 基线测试，并把编译出的二进制上传为 `redapp-<SHA>-linux-amd64`（保留 1 天） |
| `e2e`（amd64） | 下载 `runtime` 的 amd64 二进制并核对版本，按 `package-lock.json` 锁定的 Playwright 安装 Chromium，`make e2e`；失败时上传 `frontend/test-results/` 中的 trace |
| `source-image`（amd64） | 用根 Dockerfile 从源码（前端 + Go 阶段）构建镜像，检查版本并跑 Docker 测试；只验证，不产生产物 |
| `windows-installers` | 复用 `windows-installers.yml`，PS7 与 5.1 安装器门禁 |

- 只有 `PMExtra/RedApp` main 的 push 才上传 `runtime-<SHA>-<arch>` 产物（image.tar + metadata.json，保留 3 天）。PR 跑完整验证但不产生可发布产物。
- 所有 action 都固定到 commit SHA，并在行尾注释主版本（如 `# v4`）。checkout 一律 `persist-credentials: false`。新增 action 遵循同样做法（`toolchain-check` 强制）。
- Linux 任务统一使用 `ubuntu-26.04` / `ubuntu-26.04-arm`；Windows 安装器门禁使用 `windows-2022`。
- 缓存只用于加速，不能当作可信产物：源码测试的 Go 缓存按 runner/Go 版本/go.sum 分键，发布编译缓存另按 Linux 架构、`GO_IMAGE` 摘要和 go.sum 分键。

## 发布与推广

发布的信任模型是“只发布 CI 测试过的那份字节”，发布任务从不重新编译。

1. 确认准确的 main 提交 CI 全部成功。
2. 在该提交上创建 annotated tag `v<VERSION>`。
3. `publish.yml`（tag push 触发，单一 concurrency）：
   - `select-release-ci.py` 按 SHA、main、push、`ci.yml`、completed/success 选出准确的 run，要求两个架构的产物唯一且未过期。
   - 下载产物，校验 SHA256、元数据、实际镜像平台和 labels。
   - push 两个架构镜像，组装候选 manifest，按不可变摘要在双架构上验收（含无 AVX CPU 测试）。
   - `promote-release-image.py` 再次确认 main 仍等于该 SHA，然后推广 `v<版本>`、`<版本>`、`<主.次>`、`latest` 四个标签。版本标签已存在且摘要不同时失败。
4. `verify-published.yml`（main 上发布相关文件变化时）重新拉取已发布的版本标签并验证。

失败恢复：

- 产物过期或缺失：对仍在 main 上的同一提交重跑 CI，再重跑原发布任务。若 main 已前进，使用新版本和新提交，不能移动旧 tag。
- 注册表多标签写入不是原子的。推广中途网络失败时，核对四个标签后重跑同一任务，不重新编译或更换产物。
- 发布 runner 必须先安装 `qemu-user`/`binutils`，再注册 Docker binfmt；顺序颠倒会让 ARM 容器验收时报 exec format error。
- 候选和 `ci-<SHA>` 架构标签目前没有自动清理策略。

## Schema 迁移

规则见 [ADR 0001](adr/0001-pre-1.0-no-migrations.md)。框架在 `internal/store/migrate.go`：

| 内容 | 位置 |
| --- | --- |
| 当前 schema 与版本 | `schema.sql`、`store.SchemaVersion` |
| 最低可迁移版本（`0` 表示不支持迁移，1.0 前保持 `0`） | `store.MinimumMigratableVersion` |
| 迁移步骤（`{From, Name, Up(tx)}`，按版本顺序，每步升一个版本） | `migrate.go` 的 `migrations` |
| 每个 schema 版本的 golden fixture（带代表性数据的 SQL dump） | `internal/store/testdata/schema/v<N>.sql` |

启用后，`Open` 遇到 `MinimumMigratableVersion` 到 `SchemaVersion-1` 之间的数据库时：

1. 只读预检（`Preflight`）识别版本；比当前新（降级）或低于最低版本的数据库被拒绝，文件不变。
2. 在实例锁下检查剩余空间（至少为数据库有效大小的两倍：一份备份、一份迁移 WAL），不足时拒绝，不写任何文件。
3. 用 `VACUUM INTO` 写备份 `<data>/state.sqlite.schema<旧版本>-<UTC 时间>.backup`（`0600`，fsync 文件和目录）。不覆盖已有文件；写入失败（含磁盘满）时删除不完整的备份并拒绝启动。备份从不自动删除。
4. 关闭外键约束，在一个事务中执行全部步骤，然后 `PRAGMA foreign_key_check`、写入新的 `user_version`、把 `sqlite_schema`（去掉注释、多余空白和普通标识符的引号）与全新创建的 schema 逐项比对，全部通过才提交；随后重新开启外键并 checkpoint 到主文件。
5. 任何一步失败都回滚，数据库文件逐字节不变；进程在事务中崩溃时同样只留下原数据库和备份。

1.0 前 schema 变化时：修改 `schema.sql`，提升 `SchemaVersion`，用下面的命令生成新版本的 fixture，删除旧版本的 fixture（1.0 前的旧 fixture 只验证被拒绝，没有保留价值）。

```sh
go test ./internal/store -run TestSchemaFixtureOfCurrentVersion -update-schema-fixture
```

`TestSchemaFixtureOfCurrentVersion` 要求当前版本的 fixture 存在且与 `schema.sql` 一致；`TestSchemaFixturesOfOlderVersions` 对每个旧 fixture 验证：可迁移的迁移后与全新 schema 一致且数据可读，其余被拒绝且文件不变。

### 发布 1.0

1. 确认 `testdata/schema/v<SchemaVersion>.sql` 存在且测试通过；从此不再删除任何 fixture。
2. 把 `MinimumMigratableVersion` 设为当前的 `SchemaVersion`。
3. 改写 `store.ErrIncompatibleDirectory` 的文本（去掉“Data is never migrated”），在用户运维文档（中英）中说明升级时的自动备份文件、所需空间和被拒绝的情形（降级、过旧版本）。
4. 在 ADR 0001 顶部注明 1.0 已发布、迁移已启用。

### 1.0 后新增迁移

1. 修改 `schema.sql`，`SchemaVersion` 加 1。
2. 在 `migrations` 末尾追加 `{From: <旧版本>, Name: ..., Up: ...}`。表结构变化按 SQLite 的重建流程写（建新表、复制、删旧表、改名、重建索引）；步骤里不提交事务、不改 `user_version`、不碰文件。
3. 生成新版本的 fixture，保留旧版本的 fixture；`TestSchemaFixturesOfOlderVersions` 会把每个旧 fixture 迁移到新 schema 并逐项比对。
4. 步骤转换数据时，另写测试从旧 fixture 迁移并断言转换后的值。
5. 只在确实无法继续支持时才提高 `MinimumMigratableVersion`（同时删除更早的步骤），并在发布说明中写明需要先经过哪个中间版本。

## 版本号

- `VERSION` 是唯一版本来源。1.0 前不为每个小改动提升版本；只在准备发布时提升。
- 数据 schema 版本与 `VERSION` 独立，规则见 [ADR 0001](adr/0001-pre-1.0-no-migrations.md)。
