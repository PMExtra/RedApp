# RedApp 实现与验收记录

日期：2026-09-30。验证平台：Linux/amd64、Go 1.25.1；使用 CLI、确定性本地 HTTP 上游和进程测试。

## 设计基线与实现决策

设计基线见 [design.md](design.md)，官方来源和核验边界见 [upstream-contract.md](upstream-contract.md)。公开版本清除了协作元数据与私人会话叙述，功能要求、架构和验收准则保留。

企业安装器**抑制自动更新标记，CLI 二进制保持官方原样**。安装时删除已有 marker，不写入新 marker。保留官方交互开关和正常迁移确认语义，自动测试显式设置非交互。运行期更新检查和交互子进程仍由企业出口控制约束。

## 实现路径

- `cmd/redapp`：配置、生命周期、健康检查子命令。
- `internal/instance`：真实目录解析与内核 flock；不删除锁 inode。
- `internal/store`：SQLite WAL/FULL、schema 版本、JSON 状态记录、首次发现、管理员 hash、持久计数及有界事件。
- `internal/distributor`：固定 BASE_URL、受控重定向、HTTPS/DNS 边界、identity 编码与超时。
- `internal/download`：资源身份、顺序共享写入、独立磁盘读者、hash 发布、Range 恢复、持久代际及快照清理。
- `internal/apps/codex`：精确清单授权、重复字段拒绝、TTL/singleflight、原始元数据、版本比较。新 latest 显式发现同版本新哈希时产生新资源身份，first_seen 保持原值。
- `internal/auth`、`internal/httpserver`：本地密码/会话/CSRF/限速、可信代理、HTTP 管理 API、中文内嵌页面、磁盘与速率指标。
- `installers/codex`：固定原文、来源摘要、严格 patch、generated、许可证、Go embed。
- `scripts`：维护者更新 CLI、离线安装矩阵、真实 HTTP CLI 与本地 Docker 验证。

最小 SQLite 模型采用带种类/ID 主键的状态 records、独立版本/管理员/事件/计数表，而非逐项照搬设计中的建议表清单。资源锁内持久化当前指针；启动验证归属、hash、文件状态和孤立记录。文件系统和 SQLite 无跨系统原子事务，依靠 fsync、唯一代际路径与启动恢复保持正确性。

## 已执行命令和结果

使用 Go 1.25.1、GCC 和静态 libc 开发库，在仓库根目录执行：

```sh
export GOTOOLCHAIN=local
make test check build
python3 scripts/test-http-cli.py
sh scripts/test-docker-local.sh
node --check internal/httpserver/web/app.js
git diff --check
```

最终统一检查 exit=0：

| 命令 / 子项 | 结果 |
| --- | --- |
| `go test -race ./... -count=1 -timeout=120s` | 全部通过；Codex 1.050s、Auth 36.205s、Distributor 1.008s、Download 2.374s、HTTP 18.557s、Instance 1.015s；未报告 data race |
| `python3 scripts/test-installers.py` | 25 个真实 shell 离线场景通过；PowerShell 静态出口检查通过 |
| `python3 scripts/test-update-installers.py` | 检查模式、摘要拒绝、patch 冲突保留旧目录通过 |
| `gofmt -l` / `go vet ./...` | 无格式差异；vet 通过 |
| `make build` | 静态 Linux/amd64 二进制 `bin/redapp`；`file` 确认为 statically linked，`ldd` 无动态依赖 |
| `python3 scripts/test-http-cli.py` | 真实可执行文件的启动、healthcheck、登录、CSRF 设置、状态、脚本、SIGTERM 和重启不重复密码通过 |
| `sh scripts/test-docker-local.sh` | 从 scratch 本地构建 runtime；UID/GID 65532、只读根、持久卷、健康、双实例拒绝、SIGKILL 重启和正常停止通过；临时镜像/卷已清理 |
| `node --check ...` | 前端 JS 语法通过；没有执行 GUI 检查 |

## A01–A24 验收矩阵

“通过”指所列本地确定性测试覆盖的验收行为，不扩展为所有生产平台的兼容性声明。“未完整验证”列出已经通过的部分和剩余边界。

| 编号 | 状态 | 可复查证据 / 剩余边界 |
| --- | --- | --- |
| A01 按需回源 | 通过 | `TestMetadataLazyTTLFailureAndHistory` 拒绝预下载其它 variant；HTTP 仅下载请求的资源 |
| A02 100 并发 | 通过 | `Test100SimultaneousFirstAcquisitionsCreateOneWriter` 用同一 barrier 同时竞争首次代，100 读者、1 上游、全字节一致 |
| A03 流式共享 | 通过 | `TestSharedStreaming100LateSlowAndCancelled` 上游尾部阻塞期间两个读者已取得前缀，晚到者从 0 读取 |
| A04 慢读者 | 通过 | 同一测试保留完全不读的第 100 位读者，其他 99 位先完成；固定缓冲、落盘偏移、读者数量和采样数量有界 |
| A05 最终哈希 | 通过 | `TestHashInvalidStartsNewGeneration`、`TestEnterpriseInstallerThroughRedAppAndHashFailure`，以及 shell manifest/archive 篡改场景；服务器失效新代，真实安装器拒绝完成安装 |
| A06 正常续传 | 通过 | `TestValidatedResumeAndRejectedBranches/valid` 验证 Range/If-Range、正确 206、完整 hash；真实 SIGKILL 后同样验证剩余 range |
| A07 不支持续传 | 通过 | `/200` 拒绝追加，旧 Reader 错误结束、新代请求无 Range 完整重下 |
| A08 非法续传 | 通过 | bad-range、etag、encoding、416-bad、short-range；另有正确 416/弱 validator 分支，hash 仍是发布前提 |
| A09 重启恢复 | 通过 | `TestSIGKILLReleasesLockAndResumesDiskPrefix` 真实子进程下载前缀后强杀，立即取得锁并恢复 part |
| A10 双实例 | 通过 | `TestLockAliasDoubleInstanceAndSIGKILL`、上述进程测试和 Docker 第二实例；符号链接别名不绕过锁，锁文件保留 |
| A11 latest TTL | 通过 | TTL 内复用、singleflight、成功时间到期、刷新失败不延长、TTL 设置立即生效；同版本新 digest 不复用旧资源身份 |
| A12 永久缓存 | 通过 | 元数据历史缓存和 completed blob 重复读取不回源；清理仅移除资源代，first_seen 及授权历史保留 |
| A13 白名单 | 通过 | 元数据坏 URL/重复名/摘要拒绝、固定 origin/path/query 验证、私有 IP 拒绝、未知 HTTP 资产和 Host 注入拒绝；额外生产 CDN 尚未授权 |
| A14 清理竞态 | 通过 | `TestCleanupOldWriterDrainsNewGenerationSurvives` 旧 writer 阻塞时清理，新代先完成，旧代排空不能覆盖新代；旧租约归零后回收 |
| A15 清理崩溃 | 通过 | `TestCleanupProcessCrashWindows` 的 14 个确定性退出窗口，每个两次重启并重放旧任务；旧代回收、current 清理、first_seen 保留、新代保护均通过；详见下节。真实机器断电/存储设备失效未模拟 |
| A16 版本边界 | 通过 | 稳定/alpha/beta/数字排序/前缀、溢出/非法阈值拒绝；不能解释的历史版本保留并报告。按官方受限输入，rc 示例不作为当前安装输入支持声明 |
| A17 管理安全 | 通过 | bootstrap 一次、bcrypt hash、Cookie、安全属性、登录限速、CSRF、改密注销、退出；真实 CLI 重启不重复密码 |
| A18 代理安全 | 通过 | 不可信头忽略、右到左可信链、IPv6/引号、Forwarded 优先、畸形不拼链；host/proto 用固定配置，Host 注入拒绝 |
| A19 指标 | 通过 | 两次下游共享一次上游的持久字节、cache reuse、版本计数、文件系统余量、逻辑/已分配磁盘分类；`TestEffectiveAverageExcludesVerificationAndRecentSnapshot` 验证平均排除验证及采样时间 |
| A20 脚本更新 | 未完整验证 | pinned digest、严格 patch、原子目录交换实现、失败保留旧目录、25 个 shell 场景和服务安装链通过；Windows PS 5/7 实测、真实 macOS 实测、新版联网 fetch 未执行成功 |
| A21 Docker | 通过 | v0.1.0 的 [托管 CI](https://github.com/PMExtra/RedApp/actions/runs/36788207268) 完成全 Dockerfile 构建及 runtime 检查；[发布工作流](https://github.com/PMExtra/RedApp/actions/runs/36788716836) 按 digest 重新拉取并验证版本、持久化、健康、双实例和崩溃恢复。仅 Linux/amd64；匿名 GHCR 访问未验证 |
| A22 资源边界 | 通过 | 读者/writer 上限、`/dev/full` 写失败注入、已有缓存不受污染、SQLite busy timeout、源站挂起有限失败；未执行真实卷满和超大流量压力基准 |
| A23 故障恢复 | 通过 | rename 未提交、坏 blob、part、tombstone、真实进程强杀恢复；恢复后重新 hash，不能仅凭 DB 标记 complete |
| A24 架构边界 | 通过 | distributor/download/store 不导入 Codex 元数据/版本解析；唯一静态 Codex 模块、固定请求目标、无插件/第二应用/外部运行服务 |

## 交付前有限复核与 A15 补测

最终统计：**24 项验收中 23 项通过、0 项失败、1 项未完整验证（A20）**。此统计限定于上表的测试范围。

A15 覆盖的具体故障窗口为：预览记录已提交但未开始清理；退休 tombstone 已提交但 current 尚未删除；current 已删除但内存尚未分离；内存已分离但文件未删除；blob/part 已 unlink 但 generation 尚未删除；generation 删除已提交但清理任务尚未删除；任务已删除但仍被旧读者租约持有；以及新代已发布后旧代最终回收的对应窗口。这些窗口均有确定性故障测试。

`TestCleanupProcessCrashWindows` 启动独立测试子进程，取得真实 flock/SQLite/缓存，在下列准确提交点直接 `os.Exit(91)`，不调用任何 Close/恢复 defer。父进程验证退出码后，立即重新取得锁并连续重启两次；检查当前代、旧文件、旧记录、历史 first_seen 和遗留任务重放。故障屏障为包内未导出的测试字段，生产始终 nil，没有配置/HTTP 入口。

| 故障屏障 | 场景数 | 已验证恢复结果 |
| --- | ---: | --- |
| `preview.after_job_save` | 1 | 仅预览不退休，首次重启保留旧 current；执行原任务后下次重启无旧代 |
| `cleanup.after_tombstone` | 1 | 删除遗留旧 current 和退休对象 |
| `cleanup.after_pointer_delete` | 1 | 没有指针的旧代幂等回收 |
| `cleanup.after_detach` | 1 | 内存分离后的持久恢复不出现 dangling complete |
| `delete.before_files` | 2 | 旧代未删除/新代已存在两种状态；只删旧路径 |
| `delete.after_blob_unlink` | 2 | 缺失旧文件可重放，不误删新 blob |
| `delete.after_part_unlink` | 1 | 已退休完成 part 状态夹具，新 blob 保留 |
| `delete.after_files` | 2 | 文件删除完成但数据库记录仍在，重启收敛 |
| `delete.after_generation_delete` | 2 | 数据库代已删除、旧任务仍在，任务重放不选择新代 |
| `cleanup.after_job_delete` | 1 | 清理任务已移除、旧租约尚存，进程退出后回收旧代 |
| 合计 | **14** | 每个场景连续两次重启通过 |

part 删除测试以真实完成对象重命名并持久化为合法退休 `.part` 夹具，隔离文件/数据库删除边界；旧 writer 排空后不发布的真实并发流程由 `TestCleanupOldWriterDrainsNewGenerationSurvives` 覆盖。另有真实 SIGKILL 下载前缀/续传和锁释放测试。以上模拟进程崩溃，没有模拟内核/文件系统/存储设备断电，不能声称已证明任意硬件损坏下的数据耐久性。

重点代码复核结论及修正：

1. **共享回源和响应安全**：资源代仅一个顺序 writer；读者偏移独立，取消读者不取消源请求。206 必须精确匹配 offset/end/total/声明长度，ETag 冲突拒绝追加；不安全续传创建唯一新代，旧读者错误终止。416 仅在当前长度与完整 SHA256 都满足时接受。验证失败由 Reader 返回错误，HTTP 中止响应，不追加错误文本。修正底层已发布文件的短读 EOF，统一返回 `io.ErrUnexpectedEOF`，避免伪装成功完成；新测试 `TestPublishedPrefixShortReadFails` 通过。完整安装器仍验证本地制品摘要。
2. **清理代际及持久化失败**：持久 tombstone 优先于删除 current；快照绑定 generation ID，旧 writer 的发布检查阻止复活；删除只使用旧代专属路径，要求 writer/reader 均排空。修正 tombstone 写失败时还原内存 Retired，generation 删除失败时保留内存记录以便原任务重试。`TestCleanupTombstoneFailureDoesNotRetireCurrent`、`TestCleanupDatabaseDeleteFailureRetainsRetryableGeneration` 及 14 个崩溃场景通过。清理 freed 计数在文件删除与计数提交之间崩溃可低估，属于运维统计，不作为回收正确性依据。
3. **锁 inode 生命周期**：Acquire 解析真实数据目录，在常驻 instance.lock fd 上使用非阻塞 flock；Close 不 unlink、不重建锁文件。补充 `O_NOFOLLOW|O_NONBLOCK` 和 regular-file 检查，拒绝 symlink/FIFO 锁文件，避免跟随替代 inode 或阻塞 open；正常多次取得锁保持同一 inode，SIGKILL 自动释放。数据目录及父目录必须由部署管理员保护，运行期间外部进程擅自删除锁 inode 不属于支持的使用方式。
4. **元数据白名单**：完整 JSON 重复键拒绝，有限大小/嵌套/资产数；每项资产必须匹配固定 origin、规范版本、精确名称与 trusted SHA256，任意查询/其它 origin/穿越均失败关闭。无 request URL 转发入口；所有 DNS 结果先检查后拨已验证 IP，忽略环境代理。新增拒绝 broadcast、CGNAT 100.64/10、benchmark 198.18/15，包括 IPv4-mapped IPv6；测试通过。只允许固定上游，未放宽未知 CDN。
5. **代理信任**：仅实际 socket 对端位于配置 CIDR 时读取转发链，右向左跳过可信代理，遇第一个不可信节点停止；畸形 Forwarded 不回退拼接 XFF；公开 host/proto 从配置取得。修正空值重复键检查和 Go 字符串转义误解析，采用 RFC quoted-pair 逐字符处理；新增畸形键、空值、Go hex 转义及尾随垃圾测试通过。不得将所有公网地址配置为可信代理。

本轮实际执行：

```sh
go test -race ./internal/download ./internal/distributor ./internal/instance ./internal/httpserver -count=1 -timeout=120s
# 全部通过：Download 2.688s，Distributor 1.011s，Instance 1.023s，HTTP 17.653s
go test -race ./internal/download ./internal/apps/codex -count=1 -timeout=90s
# 全部通过：Download 2.627s，Codex 1.051s；含新增数据库删除拒绝及元数据白名单
make check build
python3 scripts/test-http-cli.py
sh scripts/test-docker-local.sh
# vet、格式、静态构建、真实 CLI、Docker runtime 均 exit=0
```

v0.1.0 的完整多阶段 Dockerfile 已在干净的 Linux/amd64 托管 runner 上构建并通过容器验证；发布后的镜像按 digest 重新拉取和运行验证通过。证据见 A21 中的 CI 与发布链接。匿名拉取和其他架构不包含在此验证结论中。

## 尚未验证与发布门禁

仍需完成：真实原始 release/manifest 校验样本、旧 legacy 和 alpha/beta 样本、最小 CDN 重定向/Range 实探、Windows/macOS 安装与失败路径、六平台制品内许可材料。服务对未核验 origin 失败关闭。

官方页面文本显示固定 origin 的资产 URL，与当前解析规则相符；文本观察不代替原始字节 fixture 或实际制品链验证。[0.159.2 官方清单](https://releases.openai.com/codex/releases/0.159.2/release.json)。


## 默认数据目录修订验证（2026-10-01）

程序、Docker 和宿主系统服务的默认路径统一为 `/var/lib/redapp`，开发显式 `--data ./data`。新增 `python3 scripts/test-data-cli.py` 已验证默认/空环境值、环境与参数覆盖优先级、数据库写入、显式开发路径、权限错误直接失败且不回退。`make check test build`（含全量 race、25 个 shell 离线安装场景与更新器检查）、HTTP CLI、actionlint、shell 语法及双语命令/文档链接检查通过。

`sh scripts/test-docker-local.sh` 使用本地静态二进制离线重建 runtime，已验证 `/var/lib/redapp` 的新空命名卷首次启动与数据库初始化、非 root/只读根、健康检查、双实例拒绝、SIGKILL 恢复、容器重建后数据库保持；重建时清空 `REDAPP_DATA`，独立验证程序默认路径。目录以 UID/GID 65532、0700 预建，不使用 root 入口修正 bind mount。

本地验证阶段的完整 Dockerfile 构建在解析 `golang:1.25.1-bookworm` 基础镜像 metadata 时被 Docker Hub HTTP 429 限制，当时未验证完整 builder；离线 runtime 验证不能替代该检查。现有 CI 已接入新测试，v0.2.0 的发布门禁要求确切提交完成完整 Dockerfile 构建和 runtime 检查，再按发布 digest 拉取并重复验证。旧 v0.1.0 仍使用 `/data`，不可视为新路径镜像。上述结果不改变 Windows/macOS 和真实生产上游的既有未验证边界。


## 双架构镜像修订（本地验证，待 CI）

目标仅 Linux/amd64 与 Linux/arm64（aarch64）。Dockerfile 使用 `BUILDPLATFORM` 原生 Go 编译器，明确 `TARGETOS/TARGETARCH`，保留 CGO SQLite 并按需安装目标 GCC/static libc；拒绝其它目标。CI 增加 `ubuntu-24.04` 与 `ubuntu-24.04-arm` 原生矩阵，两者都执行 race/CLI、完整镜像构建和 runtime；发布构建双架构 manifest，并按同一 index digest 分别拉取和运行两种变体（arm64 使用 QEMU）。这些门禁尚未运行，不当作通过。

本地 `REDAPP_TEST_PLATFORM=linux/amd64 sh scripts/test-docker-local.sh` 通过，涵盖版本、架构检查、非 root/只读根、`/var/lib/redapp` 新空卷、数据库初始化/重建持久性、健康、双实例拒绝与 SIGKILL 恢复。actionlint、shell 语法、双语命令与文档链接检查通过。此次 amd64 runtime 使用既有本地静态二进制重建 scratch runtime，不是新的完整 builder 结果。

ARM64 完整 Dockerfile 构建已尝试：官方 Go 基础镜像可拉取，但 `RUN go mod download` 在构建容器中解析 `proxy.golang.org` 的 DNS 返回 connection refused；尚未进入目标 C 编译器安装/编译。改用 host network 后同样失败。已取得用户态 QEMU，但尚无构建成功的 ARM64 制品，因此 **ARM64 编译与实际 runtime 仍未验证**，必须待原生 ARM64 CI 和发布后同 digest 测试通过。建议下一版本 v0.2.1；已发布 v0.2.0 仍仅 amd64，没有修改旧 tag 或发布新镜像。

## 当前源码增量：全局指标历史与 Vue 无头验收（2026-10-01）

前述章节保留各次历史验证记录。当前源码已改用 Go 1.27.1、target-platform `golang:1.27.1-trixie`、`node:24.19.0-trixie-slim` 前端构建阶段以及 Ubuntu 26.04 原生 runner；不再使用此前的跨平台编译器安装分支。Vue/TypeScript 管理页全部本地 embed，运行镜像仍为 scratch。

新增 43 项固定全局指标，目录、每分钟采样/UTC 每小时聚合、24h/7d/30d 分辨率及保留期、缺失/计数器/速率语义见[指标历史](metrics-history.md)。版本/单资源保持当前状态及速度。新增 schema 为可重复创建的附加 metric 表，不改既有缓存/授权表。小时边界尚未提交时读 API 临时做小时聚合；持久维护始终先聚合后删除，失败事务回滚。修正 miss 新请求重复累加，不改写既有累计值。

| 增量检查 | 结果 | 证据 |
| --- | --- | --- |
| UTC、gauge min/max/avg/last/count、当前 partial | PASS | `internal/history/history_test.go` |
| counter 不平均、重启/下降/断档与缺失分钟不算已知增量 | PASS | 相邻分钟、实际时间与进程 boot 检查 |
| rate 完整五秒窗口、覆盖秒数、观测加权平均 | PASS | history 和 store 的确定性测试 |
| 24h raw / 30d aggregate 保留、幂等、重新打开恢复、删除失败原子回滚 | PASS | SQLite trigger 注入删除失败，raw/aggregate/watermark 全部回滚 |
| 三个 API 分辨率、固定目录拒绝任意维度、既有 session/CSRF | PASS | HTTP/race 及真实 CLI 测试 |
| 前端历史默认7d/三个窗口/空/错误重试/请求取消/未知counter增量 | PASS | 12 个前端 DOM 测试，类型检查和生产构建 |
| 真实桌面/手机交互与视觉审阅 | PASS | Chromium 151.0.7922.173、Playwright 1.57.0，1366×900 / 390×844，本地 fixture；15 组交互检查，无意外 console/network/CSP 错误，已审阅代表截图并修正手机导航间距 |
| 完整新 Dockerfile 双架构 builder | UNVERIFIED | 官方 Node/Go 镜像正常拉取仍受 Docker Hub 匿名限流，未绕过；本地 scratch/静态二进制验证不能代替完整 builder |

无头验收覆盖登录、概览全部卡片、版本资源、事件、设置、两种真实剪贴板复制、清理确认/取消、代理凭据替换/保留/清除不回显、历史打开/窗口/表格/关闭焦点返回、空与错误恢复、登录过期停止轮询。截图仅测试数据，登录密码框为空，无真实凭据；脚本删除运行数据库后退出。图表使用 uPlot 1.6.32，额外约 57 KB JS，不引入 CDN/外部服务，也不放宽 CSP。

本次不推送、打 tag 或发布镜像；Windows/macOS Codex 实机安装、真实生产上游链、其它浏览器与新代码 ARM64 运行保持未验证，不从 Chromium 后台验收推断这些结果。

## v0.4.0 公共入口与统一前端（2026-10-02 本地验证记录）

| 项目 | 结果 | 实际证据与边界 |
| --- | --- | --- |
| 公共目录、Codex 安装页及安全 origin | PASS | 匿名 HTTP、非法路径/查询、可信代理和固定 origin 测试；安装命令仍默认 CODEX_RELEASE/latest |
| 统一顶栏、后台导航、账号菜单、密码模态框 | PASS（CLI） | Vue DOM 覆盖键盘方向键/Escape/焦点返回、确认密码、失败与重复提交；真实浏览器验证键盘焦点并审阅代表截图 |
| English / 简体中文 | PASS（CLI） | 语言切换和持久化；文案插值参数一致；后端固定 43 项指标名称全部有中文映射；原始诊断数据保留原文 |
| 版本/架构页脚与本地时间快照 | PASS（CLI） | `/api/info` 仅返回 version/os/arch，验证构建版本与 Go 运行平台；时间格式使用浏览器默认时区 |
| 刷新与会话失效 | PASS（CLI） | 图标按钮含 aria-pressed；停止/恢复轮询、无并发刷新、注销后丢弃迟到响应 |
| 维护设置及代理 | PASS（CLI） | 读取真实 latest TTL；清理预览编辑后失效、重复执行拦截；代理凭据 keep/replace/clear 不回显 |
| 历史指标 | PASS（CLI） | 默认 7d、24h/30d 切换、缺口/覆盖语义、失败重试、请求取消及原有 Go 聚合测试保留；UTC 桶不随快照本地化改变 |
| 类型、格式、后端与制品构建 | PASS | `make check test frontend-test build`；21 个前端测试、完整 `go test -race ./...`、安装器离线回归；最终生成资源下 HTTP/Codex 模块 race 再验证 |
| 真实 CLI HTTP / 数据目录 | PASS | `python3 scripts/test-http-cli.py`、`python3 scripts/test-data-cli.py`；验证匿名/管理隔离、全部前端 chunk 静态服务、设置和版本接口 |
| 窄屏像素布局、浏览器及视觉验收 | PASS | Chromium 151.0.7922.173 / Playwright 1.57.0，中英文 × 1366×900/390×844 共 20 组交互检查、24 张截图，预期外错误 0；审阅全部目录/详情/概览及代表菜单、密码、历史、设置截图，未发现遮挡或整页横向溢出 |
| OpenAI 品牌标志 | PASS（接入）；来源标签已核验 | 用户提供 SVG 原文，内置 Codex 模块；保留 path/viewBox 及指定更正，不宣称 Codex 专属图标；出处与 SHA256 已记录；Commons 标记 PD-textlogo 并注明商标限制，未比对远程原件字节 |
| 新镜像、ARM64 及远端 CI | 发布门禁 | 标签只能指向已通过 main 双架构原生 CI 的精确提交；发布工作流检查 manifest 并按子 digest 拉取运行两种架构。发布结果以该提交及 v0.4.0 的 Actions 记录为准，不能沿用 v0.3.0 结果 |

既有仅英文测试已限定后端应用消息，前端双语由单独测试覆盖。`scripts/test-admin-headless.cjs` 已适配本轮结构，使用临时 loopback 服务与假缓存/历史数据，拒绝外部页面请求，退出删除运行数据库。截图前强制检查所有密码输入为空；测试凭据不写入报告。浏览器检查发现账号按钮 ArrowDown 冒泡后重复移动焦点，已通过阻止该事件冒泡修复，重新构建、前端 21 项测试及完整无头流程通过。

四种组合均验证本地 SVG、语言持久化、真实剪贴板命令、浏览器本地时区、43 项指标、自动刷新、账号菜单、密码失败/重复提交/成功后注销、历史与维护导航、TTL、代理凭据动作及会话失效；另验证历史错误重试/空态和最终清理执行。24 张截图仅含测试数据；模型审阅是视觉检查，不能替代其它浏览器、真实移动设备或 Windows/macOS Codex 安装验证。

## v0.4.1 站点设置、交互与流量口径

本节是 2026-10-02 的 v0.4.1 本地及发布前验证记录。远端 CI、双架构镜像及发布状态以本版本对应 Actions 为准。v0.4.0 的浏览器与镜像结果不适用于本节。需求及存储契约见 [v0.4.1 说明](frontend-v0.4.1.md)。

| 要求 | 结果 | 本轮证据与边界 |
| --- | --- | --- |
| 双语默认副标题 | PASS（CLI） | `site/settings_test.go`、`AppShell.test.ts` 与真实 HTTP CLI 验证英文 Application Redistribution Platform / 中文 应用再分发平台 |
| 站点文案可编辑、兼容、持久化 | PASS（CLI） | 旧目录/缺少字段使用默认值；合法保存、无效更新不覆盖、重新打开数据库；HTTP CLI 真实进程重启后保持；前端迟到响应不覆盖刚保存值 |
| 公共导航与内容对齐 | PASS（浏览器与视觉） | CSS 共用 1200px 宽度与响应式留白；中英 1366px / 390px 视口测量顶栏/正文/页脚偏差不超过 1px，代表截图审阅通过 |
| 通用可编辑声明与纯文本边界 | PASS（CLI） | 单一页脚声明、双语保存、HTML 注入作为文本显示；未登录 401、缺 CSRF 403、非法输入 400；公共 JSON no-store 且无会话 Cookie |
| 公共隐藏架构，后台版本后括号显示 | PASS（CLI） | AppShell DOM 分别断言公共不显示架构、后台 `v0.4.1 (linux/arm64)`，无架构标签 |
| 复制按钮能力检测与失败提示 | PASS（CLI） | 不安全上下文或缺 Clipboard API 隐藏按钮，命令仍可选择；DOM 权限失败显示行内状态；真实 Chromium 安全上下文复制准确命令，非安全 origin 隐藏按钮，未覆盖全部权限策略 |
| 面向终端用户的安装页 | PASS（CLI） | PublicApp DOM 确认去除重复声明、脚本审查与 IT 推广说明；必要可信服务/摘要/登录提示保留，管理员详情在运维和安装文档 |
| 所有下拉框统一 | PASS（CLI/浏览器），真实辅助技术 UNVERIFIED | 语言、版本、代理凭据、历史计数视图四处替换；源码扫描无原生 select；方向键、Home/End、字母查找、Enter、Tab、Escape、外部点击、焦点、禁用与 ARIA 关系测试；历史弹窗内取消/选择回归 |
| 流量压缩口径及失败计数 | PASS（CLI） | identity/透明解压拒绝；压缩归档按包体计数；100 共享读者、缓存复用、哈希失败、Range 七分支、写盘/长度失败、重试与下游部分写失败；外层 gzip 不改变压缩前下行计数，旧累计保留 |
| 固定 RedApp GitHub 链接 | PASS（CLI） | 公共/后台共用固定地址，改站点标题及注入文本不改变项目链接 |

实际执行并通过（exit 0）：

```sh
make check test frontend-test build
# gofmt、go vet、全量 go test -race ./...（9 个含测试包）
# 25 个 shell 离线安装场景；PowerShell 静态检查；更新器失败保护回归
# 前端类型检查与 12 个文件 / 30 项 DOM 测试；生产资源与 Linux/amd64 静态二进制

# 补齐历史弹窗下拉框和关闭状态的 ARIA 关系后，仅重跑受影响的前端检查和构建：
make frontend-test build
python3 scripts/test-http-cli.py
python3 scripts/test-data-cli.py
node --check scripts/test-admin-headless.cjs
git diff --check
```

HTTP CLI 验证新生成的全部静态资源、匿名/管理 API 隔离、双语文案及带 HTML 字符文本的保存与重启保持。数据目录 CLI 验证默认值、环境/参数优先级及权限失败不回退。本地构建输出为静态链接 Linux/amd64 的 `RedApp 0.4.1`；发布构建另注入确切提交 revision 并执行镜像门禁。

补充静态检查通过：48 个相对文档链接/锚点、无原生 select 元素、变更文件清单与明显密钥模式；最终 7 个嵌入前端文件重新构建后 SHA256 逐项一致。

初期 CLI 验证之后补充了下述浏览器验收；真实读屏、Windows/macOS 实机安装和生产回源链仍未验证。本地没有执行新 Docker/ARM64 验证，双架构构建和运行使用发布前后的 Actions 门禁。安装器原文、patch、generated 和依赖保持原样。流量不含 HTTP/TLS/网络开销，旧版失败字节漏计不回补，数据库故障/崩溃计数差异不参与完整性判断，详见[指标口径](metrics-history.md#制品流量与压缩口径)。

### v0.4.1 浏览器验收

```sh
REDAPP_TEST_ARTIFACT_DIR=/tmp/redapp-v041-screenshots \
  node scripts/test-admin-headless.cjs
```

Chromium 151.0.7922.173 / Playwright 1.62.1，English / 简体中文 × 1366×900 / 390×844，28 组交互检查通过，52 张截图，预期外控制台/网络错误为 0。浏览器访问仅限本地 fixture，不安装 Codex，不接触生产部署；非安全 `http://redapp.test` origin 的请求通过测试路由送往同一 loopback 服务，未使用公网域名或降低浏览器安全设置。

覆盖公共目录/安装页、登录/概览/资源/事件/设置、账号和密码交互、历史图表、四处下拉框键盘焦点/重复开关/外部关闭、弹窗内 Escape 分层关闭、精确剪贴板命令、非安全上下文隐藏复制、流量压缩标签与固定源码链接。双语标题/副标题/声明通过设置页保存，重复提交只发送一次；HTML 形状文本按字面显示，真实服务进程重启后保持。公共顶栏/正文/页脚对齐测量和整页无横向溢出检查均通过。

已审阅中英公共页，以及后台概览、站点设置、资源、事件、下拉框和历史弹窗的代表截图。仅使用测试数据，截图前检查密码输入为空；临时数据库随脚本退出删除。全页截图先回到页面顶部，避免滚动时固定元素的捕获残影。验证范围是 Chromium 与窄屏视口，不等同于真实移动设备/触屏或屏幕阅读器认证；生产上游、制品许可和 Windows/macOS 安装限制保持不变。

## v0.5.0 Claude Code 与每日安装器维护

2026-10-02 本地 CLI 与发布前 Actions 验证；v0.4.1 的镜像/浏览器结果不作为本轮通过证据。下表为新增验收范围，原 A1–A24 及历史版本记录保持原样。共 **20 项：15 PASS，4 UNVERIFIED，1 EXEMPT，0 已知 FAIL**；UNVERIFIED 中包含已取得部分证据但未覆盖完整范围的项目。发布仍须最终精确提交 CI 与标签镜像门禁通过。

| 编号 | 验收项 | 结果 | 证据与边界 |
| --- | --- | --- | --- |
| V501 | 五份原始材料身份及分离签名 | PASS | 逐份字节数/SHA256 和仓库副本一致；独立 GPG 验签、固定指纹；Go 验签接受真实 2.1.285 fixture |
| V502 | 签名先于授权、算法/密钥边界 | PASS | 原文变动、签名篡改、空签名、不同公钥、SHA256 算法及额外签名包拒绝，不回源制品 |
| V503 | 清单资源白名单 | PASS | 版本、平台、文件名、大小、摘要、重复 JSON 键、遍历/查询拒绝；未签名压缩资源返回 404 |
| V504 | 渠道 TTL、惰性加载及原字节持久化 | PASS | 并发 singleflight、latest/stable 独立、TTL 保存、过期渠道失败不提供旧值；规范版本不自动过期；损坏清单重新验签 |
| V505 | 双应用共享下载及资源隔离 | PASS | 两假上游同名版本、各 20 并发读者各一次回源；缓存身份、来源/应用核对、共享读者限额；全量既有 Range/哈希/代际测试保持通过 |
| V506 | 清理、重启及旧快照保护 | PASS | 一个应用旧读者排空、新代与另一应用不受影响；重启仍复用两应用完整缓存；已完成旧任务重放被拒绝 |
| V507 | 旧 SQLite 兼容及 first_seen 原子性 | PASS | schema 1→2 保留 Codex 历史/资源 ID，重复打开不重写首次时间；迁移失败回滚；历史插入故障后重启无半份清单 |
| V508 | 两应用共享代理且边界独立 | PASS | 两轮代理切换都被 sibling 使用；固定 origin/path 交叉访问拒绝，既有 TLS/代理凭据测试通过 |
| V509 | HTTP 与真实进程管理 API | PASS | 原始 signed manifest/signature 原样返回；两个安装器注入请求 origin；未知应用拒绝，两个 TTL/清理预览隔离；真实启动、登录/CSRF、全部静态资源、重启与健康检查通过 |
| V510 | 中英文公共页与管理筛选 | PASS（DOM） | 前端 13 个文件/33 项测试；Claude 无 Codex 图标、命令/更新边界双语一致；同版本历史/资源/计数隔离，切应用清空旧清理预览 |
| V511 | Claude Shell 安装行为 | PASS（无害桩） | 64 场景：模拟 7 种平台/架构，默认/latest/stable/指定版本、jq/内置解析、正常/重复/升级；新终端设置环境、参数/退出码透传；摘要失败保留旧入口，无重定向和公网回退，冲突/临时清理通过 |
| V512 | 原文、patch、generated 与 CLI 更新失败保护 | PASS | 两应用逐字节严格 patch 一致；Codex 25 场景回归；Claude 检查模式不改目录，错误摘要和上下文冲突拒绝 apply；隔离副本成功执行原子 apply 并保留文件集合；官方原文保持不变 |
| V513 | 每日检测/隔离/发布失败门禁 | PASS（本地 fixture） | 15 个维护测试：无变化/变化、四源错误汇总、基线身份、HTML/重定向/格式、真实本地 TLS 多跳/错误证书、降级/私网/超限拒绝、严格 patch、测试/解析器失败、输出篡改、包摘要/白名单、人工分支/非草稿/main 前进拒绝、普通快进与幂等；本地 bare git + 假 PR API |
| V514 | 全量格式、静态检查、race 与构建 | PASS | gofmt、vet、全量 race 10 个含测试包、前端类型与生产构建、Linux/amd64 静态二进制、数据目录 CLI；新增 workflow actionlint 通过 |
| V515 | PowerShell 解析与 Windows 原生安装 | PASS（无害桩） | Windows Server 2022 上 pwsh / Windows PowerShell 5.1 各 16 场景，解析、正常/重复/升级、参数/退出码/环境恢复、坏摘要/渠道/清单/下载/跳转/冲突/重解析点与临时清理；实际 AMD64，ARM64 仅路径模拟；未执行官方二进制或注入入口替换间崩溃 |
| V516 | 真实 Claude 二进制与生产分发链 | UNVERIFIED | 未取得/执行官方二进制，未验证其实际 install/update/doctor、版本/渠道实时状态和生产端到端；无害桩只证明安装器控制流和环境传递 |
| V517 | macOS 原生运行、权限和签名 | UNVERIFIED | 平台模拟不代表 macOS 实机通过；未改变二进制，未执行真实原生签名/权限验证 |
| V518 | 新容器/双架构及自动维护线上运行 | UNVERIFIED（部分通过） | Linux amd64/arm64 原生 CI、完整 Dockerfile 构建和容器运行已通过；真实官方维护检测四份无变化并成功。无变化分支跳过维护容器与草稿 PR，不能证明真实变化分支的容器构建/隔离执行和 PR 写权限；标签镜像另由发布 Actions 按 digest 验证 |
| V519 | 截图与视觉验收 | EXEMPT | 从本轮起按项目决定免除截图/视觉发布门禁；继续非 GUI DOM/CLI 自动化，不声称通过真实辅助技术认证 |
| V520 | Claude 材料公开再分发许可 | UNVERIFIED | 原商业许可说明和来源已保留；不是 RedApp MIT，未代用户接受条款，未核实额外再分发授权；不捆绑 Claude 二进制 |

实际执行（通过）：

```sh
make check test build frontend-test
# gofmt / vet / 全量 go test -race ./... -count=1 -timeout=120s
# Codex 25 场景与失败保护，Claude 64 场景，维护工具测试
# Vue 类型检查、13 文件 / 33 DOM 测试、嵌入资源与静态二进制
python3 scripts/test-update-claude-installers.py
python3 scripts/test-installer-maintenance.py
python3 scripts/test-data-cli.py
python3 scripts/test-http-cli.py
# 补充故障/签名/重启用例后重跑受影响包：
go test -race ./internal/apps/claude ./internal/download -count=1 -timeout=120s
make check
# 安装器生成一致性包含于上述维护测试，不访问官方源
bash -n installers/claude-code/generated/install.sh
sh -n installers/codex/generated/install.sh
actionlint .github/workflows/installer-updates.yml
git diff --check
```

首次构建仍读取未修改的 VERSION=0.4.1，随后以 `make VERSION=0.5.0-dev REVISION=<本地基线>-dirty build` 标识本地开发二进制；发布准备已将 VERSION 更新为 0.5.0，标签与镜像须等待精确提交 CI 通过。CLI 假服务只监听 loopback，临时凭据/数据库不入库。上述有限平台/无害桩验证不代表真实 Claude、许可或生产环境验收完成。

### v0.5.0 发布前 Actions 证据

- [Linux 双架构 CI](https://github.com/PMExtra/RedApp/actions/runs/37035726711)：两个原生 Linux job 的格式、vet、race、安装器、DOM、CLI、完整 Docker 构建和容器运行均成功。该次整体 CI 因 Windows 测试夹具失败而失败，不据此发布。
- [Windows 修复验证](https://github.com/PMExtra/RedApp/actions/runs/37036873066/job/110937133973)：两种 PowerShell 各 16 场景通过。实际对照发现 Python 中转继承 PowerShell 7 模块路径时 `Get-FileHash` 无法加载，移除子进程的 `PSModulePath` 让 Windows PowerShell 重建默认路径后正常；对应[官方模块路径说明](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_psmodulepath?view=powershell-7.6)。只隔离测试环境，安装器仍强制校验 SHA256。另已修正 PowerShell 入口替换的 .NET 空值参数，并通过上述重复安装/升级测试。
- [真实官方维护检测](https://github.com/PMExtra/RedApp/actions/runs/37035726735)：四份脚本均无变化，检测成功，容器和草稿任务按设计跳过。已启用每日检测；未人为改动官方文件来制造 PR。

最终标签必须指向通过完整 main CI 的精确提交，发布工作流还须校验双架构 manifest、按 digest 拉取并运行健康/持久化测试。公开匿名拉取、真实 Claude 与维护变化分支的缺口仍单列，不用旧版本或其他分支结果代替。
