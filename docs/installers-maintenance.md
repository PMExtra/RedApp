# 安装器维护

面向终端分发的 generated 脚本仅保留简短修改声明、原有代码注释及必要的用户行为说明。项目的源码身份、patch 流程及验证过程放在本文和维护工具中，避免把维护说明混入安装制品。

官方原始基线位于 `installers/codex/upstream/`，固定 0.159.2 commit `ff6aec96948b70d94983af2641a6b67c94faeff5`；字节摘要与许可证来源记录在 `provenance.json`。禁止直接修改 upstream 原文。企业变换由 `patches/` 保存，`generated/` 必须逐字节等于原文加严格 patch 的结果。

企业脚本限制下载 origin、关闭公网回退和重定向、保留原始哈希验证，并删除自动更新 marker。CLI 二进制和官方交互确认不改动；无人值守安装显式设置 `CODEX_NON_INTERACTIVE=1`。源码基线、完整许可证和 NOTICE 独立随服务交付，不在脚本头部重复维护记录。

```sh
python3 scripts/update-installers.py --source installers/codex/upstream
python3 scripts/test-update-installers.py
python3 scripts/test-installers.py
```

检查模式不改动发布目录。更新器固定源码摘要、拒绝 patch fuzz/offset、检查网络出口并运行离线失败矩阵；仅全部成功后 `--apply` 才在同一文件系统原子替换目录。生成一致性回归核对两份 generated 的完整字节，摘要/上下文失败回归确保发布目录保持不变。PowerShell 语法在有 pwsh 的环境检查；Windows 实机测试仍需独立执行。

## 最小修改原则

满足功能与安全要求的前提下，尽量减少补丁修改范围，降低后续上游更新适配成本和风险。保留上游函数结构、格式、参数声明及有价值的原有注释；不进行全文件格式化、无关重构或批量删注释。只移除项目额外添加的维护过程说明，并将其放在本文或维护工具中。URL 约束、关闭公网回退/重定向、原始哈希校验和自动更新 marker 抑制属于必要变化，不能为缩小 diff 撤销。

当前基线补丁统计（不含 unified-diff 文件头和上下文）：

| 脚本 | 新增行 | 删除行 | 修改类别 |
| --- | ---: | ---: | --- |
| install.sh | 31 | 128 | 简短修改声明；企业 URL；下载地址/重定向约束；移除 GitHub fallback 与重新获取摘要；保留清单/包校验；抑制更新 marker |
| install.ps1 | 19 | 81 | 简短修改声明；企业 URL；请求/重定向约束；移除 GitHub fallback 与重新获取摘要；保留清单/包校验；抑制更新 marker |

删除行主要是公网回退及其 digest 重新解析分支，不是对平台识别、安装/迁移确认或解包流程的改写。本次治理恢复了两处 PowerShell 原始多行参数声明格式；没有改写 upstream，没有删除上游原有注释。每次变更均须重新确认 generated 与 patch 一致、正常安装成功、篡改/截断失败、失败关闭及交互语义保持。

## Claude Code 基线与最小 patch

Claude 原文、来源、长度与 SHA256 独立记录于 `installers/claude-code/`，不改写 Codex 基线。完整信任链和安装语义见 [Claude 说明](claude-code-v0.5.0.md)。官方原文保持逐字节不变；生成结果由严格补丁得到。

| 脚本 | 新增行 | 删除行 | 必要变更 |
| --- | ---: | ---: | --- |
| Claude install.sh | 63 | 17 | 企业 URL 和拒绝重定向；直接解析所选目标；安全临时目录；以版本文件/受管入口替代二阶段安装；入口子进程更新控制 |
| Claude install.ps1 | 69 | 32 | 同上；Windows 路径、摘要格式、重解析点/冲突检查；独立原子替换 ps1/cmd 入口；失败清理 |

没有全文件格式化、无关重构或批量删上游注释。大部分新增行实现原来由下载后二进制承担的落盘与启动入口工作。删除的是与替代安装链冲突的引导、二阶段调用和清理。仍保留有用的原始注释及必要入口所有权标识；维护细节集中在文档和工具中。

```sh
python3 scripts/update-claude-installers.py --source installers/claude-code/upstream
python3 scripts/test-claude-installers.py
python3 scripts/test-update-claude-installers.py
```

省略 `--source` 时只从受审查的两个官方 HTTPS 地址获取；默认只检查。切换到已人工核验的新原文，须显式给出 `--shell-sha256` / `--powershell-sha256`；摘要、严格 patch、静态出口和离线测试全部通过后，`--apply` 在本地同一文件系统原子交换整个模块目录并 fsync。它不提交、不推送，也不自行轮换签名公钥和许可。

## 每日官方脚本检查与草稿 PR

`.github/workflows/installer-updates.yml` 每日 **06:23 UTC** 或手动触发，明确检出 main 并固定当次提交。同一维护任务串行运行。此工作流尚未上线；安装器许可、工作流测试和发布审批仍需在推送前确认。

检测源固定在 `installers/upstream-scripts.json`：Codex 的 `https://releases.openai.com/codex/install.sh` / `.ps1`，Claude 的 `https://claude.ai/install.sh` / `.ps1`。逐一验证 main 原文字节与 provenance 一致，获取官方当前脚本，拒绝重定向、异常状态、编码、HTML、空响应和超限，计算当前摘要。这里的新脚本 SHA256 是经 HTTPS 获取后的观测值，不宣称是上游签名；Claude manifest 的二进制签名验证是独立流程。

四份检测结果统一记录在 Actions summary。全部不变时成功结束。下载或完整性检查失败仍报告其它脚本的结果，然后任务失败，不能记作无变化。有变化时依次：

1. 使用 main 中现有 patch，禁止 fuzz、offset 和上下文漂移；不自动修改 patch。
2. 在独立容器执行现有离线测试与 PowerShell 解析；容器无网络、无凭据、无 capabilities，源目录只读，限制内存/进程，只有临时空间和验证输出可写。工具镜像在执行原文之前构建。测试输出不能注入 Actions 控制命令。
3. 回到可信主机重新应用已审查 patch，比对容器生成结果，再封装有限文件和摘要。容器输出不是独立授权来源。
4. 新任务使用 main 精确提交中的发布工具，校验任务输出的包 SHA256、内容摘要、大小、路径及文件集合；不执行包中的脚本。允许变更仅限两模块的 `upstream/install.sh`、`upstream/install.ps1`、对应 `generated/` 文件及 `provenance.json`。公钥、许可证、patch、应用代码及 workflow 均不在允许集合中。
5. 使用短期 GITHUB_TOKEN 创建或更新 `automation/installer-updates` 的 **draft PR**。权限只在发布任务授予 contents:write / pull-requests:write，验证任务只有 contents:read。无 PAT、新凭据、安全设置变更、自动合并或 force push。

同一 main / 同一原文不会因检测时间变化反复提交。已有草稿以 PR 标记的精确 head 校验，分支人工提交、非草稿、未知/无 PR 分支、额外代码或 main 变化均停止，避免覆盖人工工作；更新用普通快进提交，保留历史。推送成功而 PR API 失败会留下维护分支，后续检测安全停止，由维护者处理，不偷偷删除或重建。官方源没有变化时不会自动清理旧草稿。

provenance 的 `script_baseline` 记录此次 main 和官方 live 检测时间，脚本条目的 `source` / `sha256` 是新原文身份；Codex 原 `commit` 继续表示最初固定源码与 LICENSE/NOTICE 的历史出处，不能把新 live 脚本误称为该旧 commit 的字节。

GITHUB_TOKEN 创建的 PR 可能不触发普通 PR CI，因此草稿正文明确列出每日任务真正执行的验证及 Windows/macOS/真实二进制未测项，不冒称单独 CI 已通过。若仓库禁止 token 创建 PR，失败信息要求维护者确认该权限；工具不改变设置。GitHub 通知是否送达由用户 Actions 通知设置决定，不承诺自动通知。

本地故障回归（不用外网或真实 GitHub 写操作）：

```sh
python3 scripts/test-installer-maintenance.py
```

覆盖无变化、脚本变化、所有检测错误汇总、下载/HTML/重定向拒绝、严格补丁冲突、测试失败无产物、隔离输出篡改、包白名单与摘要、草稿幂等、普通快进、人工修改/非草稿/无主分支及 main 前进保护。真实容器、GitHub 权限与首次线上运行仍须单独验收。
