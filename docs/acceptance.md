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
