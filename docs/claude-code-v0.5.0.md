# v0.5.0：Claude Code 原生分发

v0.5.0 新增 Claude Code，与 Codex 共用一个 RedApp 进程、SQLite 和下载引擎。两者是静态编译模块，分别处理自己的协议、固定上游、清单授权和版本规则；不引入插件、npm 镜像或第二个服务。既有 Codex 路径、资源身份、设置默认值及 v0.4.1 功能保持兼容。本文件描述 v0.5.0；旧 v0.4.1 镜像仅支持 Codex。

## 管理员使用

构建当前源码后，在 `/` 选择 Claude Code，安装详情位于 `/apps/claude-code`。替换下列服务地址；使用可信的企业 HTTPS 服务。

```sh
curl -fsSL 'https://redapp.example.internal/claude-code/install.sh' | bash
# 选 stable 或明确版本：
curl -fsSL 'https://redapp.example.internal/claude-code/install.sh' | bash -s -- stable
curl -fsSL 'https://redapp.example.internal/claude-code/install.sh' | bash -s -- 2.1.285
```

```powershell
irm 'https://redapp.example.internal/claude-code/install.ps1' | iex
# 显式传入目标版本：
& ([scriptblock]::Create((Invoke-WebRequest -UseBasicParsing 'https://redapp.example.internal/claude-code/install.ps1' -ErrorAction Stop).Content)) -Target stable
```

默认目标为 `latest`，也接受 `stable` 和明确版本。明确版本不先下载 latest 的引导二进制。先审查再执行时，将上述脚本保存为本地文件，确认下载成功并审查后运行 `bash install.sh stable` 或 `./install.ps1 -Target stable`；不要在下载失败后运行旧文件。

这属于 **RedApp 管理的安装**：完整校验后保留官方二进制字节，放入版本目录，创建启动入口；不执行官方二阶段 `claude install`，不使用 npm，不改写用户认证和设置文件。重复运行安装器用于更新。

| 平台 | 版本目录 | 启动入口 |
| --- | --- | --- |
| Unix | `~/.local/share/claude/versions/<version>` | `~/.local/bin/claude` |
| Windows | `%USERPROFILE%\.local\share\claude\versions\<version>.exe` | `%USERPROFILE%\.local\bin\claude.ps1` / `claude.cmd` |

入口缺少 PATH 时仅给出指引，不修改 shell profile 或系统环境。拒绝符号链接/重解析点、危险目录及非本安装拥有的现存入口，管理员须先自行迁移既有安装。重复安装验证已存在版本的摘要；升级不截断旧文件，入口单独原子替换，保留旧版本，不自动清理客户机版本或配置。Windows 两个入口分别替换，不构成跨文件原子事务。Windows CI 已验证两种 PowerShell 的参数、退出码、重复安装与摘要失败保护；仍未覆盖真实 Claude 进程占用文件或替换两个入口之间崩溃的窗口。

正常经入口启动时，每次为子进程设置 `DISABLE_UPDATES=1`；PowerShell 恢复调用者原环境，CMD 使用 setlocal，Unix 设置仅作用于 exec 子进程。直接运行版本二进制会绕过入口。依据[官方环境变量说明](https://code.claude.com/docs/en/env-vars)，该变量用于阻止自动及手动更新；本轮没有执行真实 Claude 二进制，不能将无害桩测试当作运行期更新控制的证明。用户设置、启动方式及实际版本仍需上线验收。认证、模型、插件和其它运行流量不由 RedApp 改写，不能据此保证完全离线；[官方企业网络说明](https://code.claude.com/docs/en/network-config)区分原生分发与其它服务用途。

## 原始证据与许可

2026-10-02 本地逐字节核对提供的五份原始文件，并使用独立 GPG keyring 验证 manifest 分离签名成功。它们没有包含可执行制品或渠道响应；`2.1.285` 是已验签样本版本，不代表当前 latest/stable。

| 文件 | 字节 | SHA256 |
| --- | ---: | --- |
| install.sh | 9704 | `3a68d3406cf674e17bed1733a4dcf37805e2e47d87417700007d7e1aa766a944` |
| install.ps1 | 3189 | `cd17c6b555f761d60373659824bf805e1510538226e4c7028e19d7494937a333` |
| claude-code.asc | 1688 | `bd70a5e4a268002704024ceba7f8446024114e94f3f0bdd11c23a9e592be81c6` |
| manifest.json | 2161 | `ab45eee79e59db6c01147e19b83074df8773304d790f5553b03c2a495eaa8297` |
| manifest.json.sig | 833 | `e68646c8a718f003a0c60fde9ebf1ae76646f092cc1f9484c3fab1f93015501a` |

原始安装器和公钥位于 `installers/claude-code/upstream/`；可信清单样本在 `internal/apps/claude/testdata/`。来源、长度和摘要见 [provenance.json](../installers/claude-code/provenance.json)。原脚本固定官方发行地址，总先取 latest，再运行二进制的 install 子命令；因此仅替换 BASE_URL 不足以约束安装回源。patch 保留平台识别、摘要和原有有用注释，仅适配必要下载和安装行为，见[维护文档](installers-maintenance.md)。

官方仓库的 [LICENSE.md](../installers/claude-code/upstream/LICENSE.md) 原文独立保留，适用 Anthropic 商业条款，不受 RedApp MIT 覆盖。技术上支持企业自管分发不等于取得公开再分发授权。本仓库保留原文、企业修改版和补丁，镜像嵌入企业脚本，但不捆绑 Claude 二进制。额外再分发授权尚未核实；使用或继续分发这些第三方材料时须自行核对适用条款。

## 信任与授权

服务端固定上游默认 `https://downloads.claude.ai/claude-code-releases`，管理员可用 `--claude-base-url` / `REDAPP_CLAUDE_BASE_URL` 指定另一个固定 HTTPS 源。客户端请求不能传入任意上游。统一代理设置共享同一个可切换 transport，各模块保留自己的来源和路径边界，现有请求不被切换取消。

固定签名公钥指纹：`31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE`。服务端先验原始 manifest 字节的签名，再解析授权资源。当前仅接受已核对的 OpenPGP v4 二进制 RSA-4096/SHA512 签名格式，拒绝额外签名包、未来/过期签名、其它公钥或算法；换钥与算法升级须人工复核。复用现有 `golang.org/x/crypto/openpgp`，该包已停止通用协议演进，故限定为上述固定验签格式，不作为通用 OpenPGP 服务。

manifest 和签名以原字节持久化、原字节返回，读取缓存时重新验证。已验签规范版本清单不自动过期，损坏缓存必须重新取得并验签；latest/stable 独立缓存，TTL 默认 60 秒，支持 1–86400 秒，过期刷新失败不提供旧渠道值。首次访问渠道只取渠道、清单及签名，制品按平台惰性拉取。

清单最多 1 MiB、签名 16 KiB，平台仅允许 darwin-arm64/x64、linux-arm64/x64（glibc/musl）、win32-arm64/x64；文件只能是该平台对应的 `claude` / `claude.exe`，SHA256 必须完整，大小限制 4 GiB。拒绝重复 JSON 键、版本不匹配、未知平台、遍历、查询和其它资源。仅正式 manifest 中的原始制品获准：保留官方 Shell 的可选压缩尝试结构，但服务对 `manifest.zst.json` / `claude.zst` 返回 404，安装器转到已签名清单授权的未压缩文件，不接受独立未验签压缩元数据。

客户端下载后核对 SHA256；它们不自行运行 GPG，因而信任企业 HTTPS 服务和服务端验签边界。服务端完整文件校验通过后才发布缓存。共享增长文件可能在最终校验前将字节传给等待者，所以客户端必须在安装前校验摘要；失败响应不得用于安装。重试、严格 Range 恢复、旧响应失败与新代重下沿用共享引擎。

## 应用隔离与兼容

- 元数据、渠道、TTL、版本 first_seen 按应用隔离；版本清单与首次发现历史在同一 SQLite 事务提交。schema 1→2 迁移将旧历史归入 Codex，事务失败不留下半迁移。旧资源 ID 不变，有效缓存继续复用。升级前停服务备份，旧程序不能直接读取 schema 2，降级须恢复升级前完整备份。
- 全局下载读者/写入者限额、磁盘用量、代理、认证和指标仍共用。资源 ID 继续使用固定源 URL 与摘要；授权前核对所属应用，不允许另一个应用借用缓存身份。
- 管理资源页按应用过滤；清理预览先选应用，再保存不可变代际快照。执行只作用于快照，新代、其它应用及旧读者均保持既有语义。
- `settings` / `cleanup/preview` 使用 `X-RedApp-Application: claude-code` 选择 Claude；省略则保持 Codex。未知应用拒绝。`cleanup/execute` 仍只接受精确 cleanup_id。
- 状态新增 `application_versions`，旧 `versions` 保留 Codex 含义。Codex 的版本计数键保持不变；Claude 使用 `app:claude-code:version:<version>:` 前缀，全局只计一次。
- Claude 稳定三段版本按数值排序；同一核心版本的不同 prerelease 标识保守保留并列入无法排序项，不猜测上游预发行策略。

## 本地验证与后续门禁

完整命令、结果和限制集中记录于[验收矩阵](acceptance.md#v050-claude-code-与每日安装器维护)，每日检测/草稿 PR 的权限与失败语义见[自动维护](installers-maintenance.md#每日官方脚本检查与草稿-pr)。

已用官方签名样本、两个本地假上游、无害二进制桩及 DOM 测试验证信任边界、惰性获取、应用隔离、安装参数、更新环境、摘要失败与升级保留。模拟的平台检测不等于对应实机通过。Windows Server 2022 上 PowerShell 7 和 Windows PowerShell 5.1 各 16 个无害桩离线场景已通过；AMD64 为实际主机，ARM64 仅模拟路径选择。Linux amd64/arm64 的原生 CI、完整 Docker 构建和容器运行通过。每日维护首次真实检测四份脚本无变化，故未执行有变化分支的容器或真实 PR 写入。仍缺真实 Claude 运行期、原生 macOS/Windows ARM64、生产上游端到端验证，以及维护变化分支的真实容器与 PR 权限证据。发布改用必要非 GUI 自动化门禁，截图/视觉不作为发布条件；不执行生产部署。
