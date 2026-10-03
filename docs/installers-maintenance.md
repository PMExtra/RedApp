# 安装器维护

面向终端分发的 generated 脚本仅保留简短修改声明、原有代码注释及必要的用户行为说明。项目的源码身份、patch 流程及验证过程放在本文和维护工具中，避免把维护说明混入安装制品。

官方原始基线位于 `installers/openai/codex/upstream/`，固定 0.159.2 commit `ff6aec96948b70d94983af2641a6b67c94faeff5`；字节摘要与许可证来源记录在 `provenance.json`。禁止直接修改 upstream 原文。企业变换由 `patches/` 保存，`generated/` 必须逐字节等于原文加严格 patch 的结果。

企业脚本限制下载 origin、关闭公网回退和重定向、保留原始哈希验证，并删除自动更新 marker。CLI 二进制和官方交互确认不改动；无人值守安装显式设置 `CODEX_NON_INTERACTIVE=1`。源码基线、完整许可证和 NOTICE 独立随服务交付，不在脚本头部重复维护记录。

```sh
python3 scripts/update-installers.py --application openai/codex --source installers/openai/codex/upstream
python3 scripts/test-update-installers.py
python3 scripts/test-installers.py --platform shell
```

检查模式不改动发布目录。更新器固定源码摘要、拒绝 patch fuzz/offset、检查网络出口并运行离线失败矩阵；仅全部成功后 `--apply` 才在同一文件系统原子替换目录。生成一致性回归核对两份 generated 的完整字节，摘要/上下文失败回归确保发布目录保持不变。Linux 仅验证 Shell 和生成字节一致性；PowerShell 解析及行为统一在 Windows PS7/5.1 执行。本地 `--apply` 不代表 Windows 验证或发布批准。

## 最小修改原则

满足功能与安全要求的前提下，尽量减少补丁修改范围，降低后续上游更新适配成本和风险。保留上游函数结构、格式、参数声明及有价值的原有注释；不进行全文件格式化、无关重构或批量删注释。只移除项目额外添加的维护过程说明，并将其放在本文或维护工具中。URL 约束、关闭公网回退/重定向、原始哈希校验和自动更新 marker 抑制属于必要变化，不能为缩小 diff 撤销。

当前基线补丁统计（不含 unified-diff 文件头和上下文）：

| 脚本 | 新增行 | 删除行 | 修改类别 |
| --- | ---: | ---: | --- |
| install.sh | 31 | 128 | 简短修改声明；企业 URL；下载地址/重定向约束；移除 GitHub fallback 与重新获取摘要；保留清单/包校验；抑制更新 marker |
| install.ps1 | 19 | 81 | 简短修改声明；企业 URL；请求/重定向约束；移除 GitHub fallback 与重新获取摘要；保留清单/包校验；抑制更新 marker |

删除行主要是公网回退及其 digest 重新解析分支，不是对平台识别、安装/迁移确认或解包流程的改写。本次治理恢复了两处 PowerShell 原始多行参数声明格式；没有改写 upstream，没有删除上游原有注释。每次变更均须重新确认 generated 与 patch 一致、正常安装成功、篡改/截断失败、失败关闭及交互语义保持。

## Claude Code 基线与最小 patch

Claude 原文、来源、长度与 SHA256 独立记录于 `installers/anthropic/claude-code/`，不改写 Codex 基线。完整信任链和安装语义见 [Claude 说明](claude-code-v0.5.0.md)。官方原文保持逐字节不变；生成结果由严格补丁得到。`.gitattributes` 禁止原文、生成文件、patch 和签名 fixture 的换行转换；原文既有空白及 unified diff 的空白上下文不作格式化，以保持身份与最小差异。

| 脚本 | 新增行 | 删除行 | 必要变更 |
| --- | ---: | ---: | --- |
| Claude install.sh | 63 | 17 | 企业 URL 和拒绝重定向；直接解析所选目标；安全临时目录；以版本文件/受管入口替代二阶段安装；入口子进程更新控制 |
| Claude install.ps1 | 69 | 32 | 同上；Windows 路径、摘要格式、重解析点/冲突检查；独立原子替换 ps1/cmd 入口；失败清理 |

没有全文件格式化、无关重构或批量删上游注释。大部分新增行实现原来由下载后二进制承担的落盘与启动入口工作。删除的是与替代安装链冲突的引导、二阶段调用和清理。仍保留有用的原始注释及必要入口所有权标识；维护细节集中在文档和工具中。

```sh
python3 scripts/update-installers.py --application anthropic/claude-code --source installers/anthropic/claude-code/upstream
python3 scripts/test-installers.py --platform shell --application anthropic/claude-code
python3 scripts/test-update-installers.py
```

省略 `--source` 时只从受审查的两个官方 HTTPS 地址获取；默认只检查。切换到已人工核验的新原文，须显式给出 `--shell-sha256` / `--powershell-sha256`；摘要、严格 patch、静态出口和离线测试全部通过后，`--apply` 在本地同一文件系统原子交换整个模块目录并 fsync。它不提交、不推送，也不自行轮换签名公钥和许可。

## 共同清单与服务资产

规范目录为 `installers/<vendor>/<app>/`，例如 `openai/codex` 和 `anthropic/claude-code`。共享 Go `installers` 包只嵌入 generated 脚本、许可证/NOTICE 和固定公钥；HTTP 根据应用 descriptor 授权文件，包再检查规范 ID、文件名及完整应用根，不暴露 upstream 原始脚本、patch 或 provenance。渲染只替换 `@REDAPP_BASE_URL@` 为安全公共 origin 加规范应用路径，例如 `https://downloads.example/openai/codex`。后台公共 URL 更新不改动这些已审核文件的字节或应用上游。

`installer_validator` 只选择已审核的 `codex` / `claude-code` 协议测试实现，统一由 `scripts/test-installers.py` 以固定 argv 执行；descriptor 不提供命令字符串或可加载代码。新增复用已有协议的应用时，增加 manifest 记录、对应规范目录资产和 provenance 即进入 inventory、每日检查和发布允许集合。新协议需要额外验证行为时必须审查验证器代码；加入应用、修改 patch 或轮换信任根均不属于每日自动更新权限。

## 每日官方脚本检查与草稿 PR

`.github/workflows/installer-updates.yml` 每日 **06:23 UTC**、手动触发或 main 中维护工作流、工具和 manifest 等声明路径变更时运行，明确检出 main 并固定当次提交。同一维护任务串行运行。工作流的实际运行与结果以 Actions 为准；第三方脚本的许可和来源仍独立保留，不视为已取得额外再分发授权。

唯一应用及脚本清单为 `internal/apps/builtin/manifest.json`，Go 服务和维护工具读取同一清单。检测源为受审查 descriptor 中的 `installers[].source`：Codex 的 `https://releases.openai.com/codex/install.sh` / `.ps1`，Claude 的 `https://claude.ai/install.sh` / `.ps1`。逐一验证 main 原文字节与 provenance 一致，获取官方当前脚本。维护下载器允许经有效证书验证的 HTTPS→HTTPS 跨域跳转，最多 5 次，不逐个维护目标白名单。拒绝 HTTP 降级、非 HTTPS 协议、URL 凭据/片段、重复 URL 和超限；请求前及每个目标检查 DNS，并在发送 TLS/HTTP 数据前再次检查实际 socket 对端，拒绝回环、私网、链路本地和 metadata 地址。只使用系统 TLS 信任，关闭环境代理继承，不关闭证书校验；DNS 查询结果与实际连接结果均须通过检查。写凭据任务仍不执行下载内容。终端安装器的仅 RedApp 下载边界不受此策略调整影响。异常状态、编码、HTML、空响应和超限失败，成功后计算当前摘要。这里的新脚本 SHA256 是经 HTTPS 获取后的观测值，不宣称是上游签名；Claude manifest 的二进制签名验证是独立流程。

全部 descriptor 声明的脚本检测结果统一记录在 Actions summary；数量随受审查清单变化，不在工具中固定为四份。全部不变时成功结束。下载或完整性检查失败仍报告其它脚本的结果，然后任务失败，不能记作无变化。有变化时依次：

1. 使用 main 中现有 patch，禁止 fuzz、offset 和上下文漂移；不自动修改 patch。
2. 在 Ubuntu 工具容器执行统一入口的 Shell 测试；镜像不包含 PowerShell，也不解析或执行 ps1；容器无网络、无凭据、无 capabilities，源目录只读，限制内存/进程，只有临时空间和验证输出可写。工具镜像在执行原文之前构建。测试输出不能注入 Actions 控制命令。
3. 回到可信主机重新应用已审查 patch，比对容器生成结果，再封装有限文件和摘要。容器输出不是独立授权来源。
4. 独立 Windows Server 2022 任务复用 `.github/workflows/windows-installers.yml`，检出相同 baseline 的可信 harness，核对候选 ZIP 的 SHA256、baseline、允许路径和每个文件摘要后，在临时目录以候选字节覆盖基线副本。PS7 与 Windows PowerShell 5.1 对所有声明的 ps1 进行解析及无害 EXE 行为测试。测试读取候选目录，不能回退到 checkout 中的旧文件。此任务只有 contents:read、无持久 checkout 凭据，不接收发布 token；输出禁用 Actions 命令解释。它不是 Linux 容器的网络隔离边界，也不声称 Windows 测试限制了所有潜在网络访问。
5. 仅在 Linux 和 Windows 两个任务都成功后，新任务使用 main 精确提交中的发布工具，校验任务输出的包 SHA256、内容摘要、大小、路径及文件集合；不执行包中的脚本。允许集合从同一 manifest 派生，只含已注册规范 ID 下声明的 `upstream/` 脚本、对应 `generated/` 文件及 `provenance.json`。公钥、许可证、patch、descriptor、应用代码及 workflow 均不在允许集合中。
6. 使用短期 GITHUB_TOKEN 创建或更新 `automation/installer-updates` 的 **draft PR**。权限只在发布任务授予 contents:write / pull-requests:write，验证任务只有 contents:read。无 PAT、新凭据、安全设置变更、自动合并或 force push。

同一 main / 同一原文不会因检测时间变化反复提交。已有草稿以 PR 标记的精确 head 校验，分支人工提交、非草稿、未知/无 PR 分支、额外代码或 main 变化均停止，避免覆盖人工工作；更新用普通快进提交，保留历史。推送成功而 PR API 失败会留下维护分支，后续检测安全停止，由维护者处理，不偷偷删除或重建。官方源没有变化时不会自动清理旧草稿。

provenance 的 `script_baseline` 记录此次 main 和官方 live 检测时间，脚本条目的 `source` / `sha256` 是新原文身份；Codex 原 `commit` 继续表示最初固定源码与 LICENSE/NOTICE 的历史出处，不能把新 live 脚本误称为该旧 commit 的字节。

GITHUB_TOKEN 创建/更新 PR 的普通 CI 可能需要人工批准；本流程在建 PR 之前已执行候选包的 Windows 门禁，不依赖后续 PR 事件。草稿正文区分实际执行的候选验证与原生 Windows ARM64、macOS、官方二进制等未测范围，不冒称单独 PR CI 已通过。若仓库禁止 token 创建 PR，失败信息要求维护者确认该权限；工具不改变设置。GitHub 通知是否送达由用户 Actions 通知设置决定，不承诺自动通知。

本地故障回归（不用外网或真实 GitHub 写操作）：

```sh
python3 scripts/test-installer-maintenance.py
```

本地回归覆盖 descriptor 扩展和固定验证器约束、无变化、脚本变化、所有检测错误汇总、下载/HTML/重定向边界、实际连接地址检查、真实本地 TLS 多跳与错误证书、严格补丁冲突、测试失败无产物、隔离输出篡改、包白名单与摘要、草稿幂等、普通快进、人工修改/非草稿/无主分支及 main 前进保护。

旧版维护流程在 2026-10-02 的[真实官方检测](https://github.com/PMExtra/RedApp/actions/runs/37035726735)通过；四份原文均未变化，故按设计跳过隔离容器及草稿发布。这次历史无变化结果不证明本次重构工作流已部署，也不证明有变化分支的真实容器构建或 GitHub PR 写权限；这些仍须单独验收，不为测试人为制造上游变化。

## 统一测试与平台边界（v0.6.4）

```sh
# Linux：两种协议共用入口、loopback HTTP 服务、无害制品、摘要及维护夹具
python3 scripts/test-installers.py --platform shell
python3 scripts/test-update-installers.py
python3 scripts/test-installer-maintenance.py
# Windows：默认在两个引擎解析并执行所有声明的 PowerShell 安装器
python scripts/test-installers.py --platform windows
# CI 以真实候选 ZIP 入口验证，并证明坏候选不能被旧 main 的好脚本掩盖
python scripts/test-installer-candidates.py
```

可用 `--application <vendor/app> --directory <generated-dir>` 指定单个候选，目录缺失会失败。每日更新使用 `--bundle <zip> --sha256 <digest> --baseline <commit>`，对完整候选集合验证，不能与单应用过滤混用。普通 CI 和每日候选共用同一个 Windows workflow 与 harness；普通 CI 的临时候选仅添加注释，还分别用两个引擎拒绝语法损坏候选，并确认 baseline 未变。

共享层负责应用清单、受限制品包、HTTP 记录/指定阶段重定向、Codex 新旧制品布局、无害程序和旧文件快照；Shell/Windows 适配保留真实解释器、下载器和文件系统差异。Codex 检查 package/legacy 安装、版本选择、摘要/清单/截断失败、三阶段跳转拒绝、真实 junction 切换、重装/失败升级保留旧版本/成功升级、更新 marker 删除。Windows 用完恢复测试用户 PATH，受控 PATH 不发现系统中的真实 Codex。Claude 检查两个原生启动入口的参数、退出码、环境恢复、版本和入口保护、junction 拒绝、三阶段跳转与清理。平台/目标选择与完整生命周期分开，不展开全排列。

Windows 当前机器为 AMD64；Claude ARM64 仅通过环境变量模拟选路，Codex 使用真实 OSArchitecture、不模拟原生 ARM64。Linux 上 Darwin、musl、ARM64 也只是受控平台探测结果；没有宣称在这些机器运行官方程序。所有 CLI 均为无害桩；不执行官方 Codex/Claude 二进制，不测试 GUI。upstream/patch/generated、许可证、公钥与 0.6.3 字节一致。本次只统一测试和维护工具，不引入 0.7 Provider 产品架构。
