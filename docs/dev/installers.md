# 安装器维护

RedApp 向终端分发的是官方安装脚本的企业改版：初始下载地址指向 RedApp，关闭公网回退，保留官方的哈希校验和交互语义。本文说明这些脚本从官方原文到发布的完整流水线。

## 目录与身份

每个应用一个规范目录 `installers/<vendor>/<app>/`，当前有 `openai/codex` 和 `anthropic/claude-code`：

| 路径 | 内容 | 规则 |
| --- | --- | --- |
| `upstream/` | 官方原文（含签名块）、LICENSE/NOTICE、公钥 | 逐字节保持官方原样，禁止手工修改 |
| `patches/` | 每个脚本一个 unified diff | 只包含必要的企业变换 |
| `generated/` | 实际分发的脚本 | 必须逐字节等于“原文去签名后应用 patch”的结果 |
| `provenance.json` | 来源 URL、字节数、SHA-256、获取时间 | 原文变化时由维护工具更新 |

- `.gitattributes` 对 upstream、generated、patches 和 Claude 签名 fixture 关闭换行与空白转换，Windows checkout 也不会改变字节。
- Codex `provenance.json` 的 `commit` 指最初固定的源码提交（LICENSE/NOTICE 的出处），不代表后来从 live 地址获取的脚本字节。
- Go 包 `installers`（`installers/assets.go`）只嵌入 generated 脚本、许可证/NOTICE 和固定公钥，不暴露 upstream 原文、patch 或 provenance。渲染时把 `@REDAPP_BASE_URL@` 替换为安全的公共 origin 加规范应用路径，例如 `https://downloads.example/openai/codex`。

## 企业变换的范围

坚持最小修改：保留上游函数结构、格式、参数声明和有价值的注释；不做全文件格式化、无关重构或批量删注释。generated 脚本只带简短的修改声明，维护过程说明放在本文和工具里。

| 应用 | 必要变换 |
| --- | --- |
| Codex | 企业 URL；下载地址约束；移除 GitHub 回退和重新获取摘要的分支；保留清单/包的哈希校验；抑制自动更新 marker |
| Claude Code | 企业 URL；直接解析所选目标；安全临时目录；以版本目录 + 受管入口替代官方二阶段 `claude install`；入口为子进程设置 `DISABLE_UPDATES=1` |

- CLI 二进制和官方交互确认不改动。无人值守安装 Codex 时显式设置 `CODEX_NON_INTERACTIVE=1`。
- Claude 的 manifest 签名由服务端验证（见 [architecture.md](architecture.md#提供者provider)），客户端只校验下载摘要。

## patch 规则

- 禁止 fuzz。允许整体行号偏移；任何 hunk 失败、反向或需要模糊匹配都视为冲突，工具停止并要求维护者重建 patch。
- 工具不会自动修改 patch。
- PowerShell 原文末尾可能带 Authenticode 签名块（证书有效期短，会频繁重签）。企业修改必然使签名失效，所以应用 patch 前先移除签名块，这一步不进入 patch。只移除一个位于文件末尾的签名块：从最后一个 `# SIG # Begin signature block` 起，到文件结尾处的 `# SIG # End signature block` 止，中间不得出现其他签名标记。文件其他位置出现任何签名标记都会停止并交人工审查，每日检查把这种情况报告为 `error`，不会当作无变化。
- 每日检查按“移除签名后的内容”比较：仅重签视为无变化，summary 标注 `unchanged (signature only)`，不提 PR。因此 upstream 中的签名字节可能早于官方当前签名。

## 本地维护

```sh
make installer-inventory
python3 scripts/update-installers.py --application openai/codex --source installers/openai/codex/upstream
python3 scripts/update-installers.py --application anthropic/claude-code --source installers/anthropic/claude-code/upstream
python3 scripts/test-update-installers.py
python3 scripts/test-installers.py --platform shell
```

- `make installers` 等价于上面两条 `update-installers.py`。
- 省略 `--source` 时，工具只从受审查 descriptor 中声明的官方 HTTPS 地址获取原文。默认只检查，不改动发布目录。
- 切换到已人工核验的新原文时，必须用 `--shell-sha256` / `--powershell-sha256` 给出期望摘要，此时按含签名的完整字节核对。
- 不给摘要时，本地检查与每日维护口径一致：已提交原文须匹配 provenance 摘要；新来源若与其仅差签名块，视为无变化并保留已审计的字节。
- `--apply` 仅在摘要、无冲突 patch、静态出口审计和离线测试全部通过后，在同一文件系统上原子替换整个应用目录并 fsync。它不提交、不推送，也不轮换公钥或许可。
- 本地 `--apply` 不代表 Windows 验证通过，也不代表发布批准。

## 清单与 inventory

- 唯一的应用清单是根目录 `presets/` 下的逐实体 YAML。descriptor 的 `installers[].source` 声明官方脚本地址；`distribution` 声明分发资产。
- `make installer-inventory` 用 Go 解析器（`cmd/preset-inventory`）导出 `.generated/installer-inventory.json`。Python 工具只消费这份生成结果，不重复解析 YAML。该文件不提交。
- 安装器行为测试由 `scripts/test-installers.py` 以固定 argv 执行，只能选择已审核的 `codex` / `claude-code` 协议实现。descriptor 不能提供命令字符串或可加载代码。
- 新应用复用已有协议时：增加 preset 中的 distribution 声明、对应规范目录资产和 provenance，即自动进入 inventory、每日检查和发布允许集合。新协议必须审查验证器代码。

## 每日检查与草稿 PR

`.github/workflows/installer-updates.yml` 在每日 06:23 UTC、手动触发，或 main 上相关路径（工作流、安装器原文与工具脚本、`Makefile`、`presets/**`、`cmd/preset-inventory/**`、`internal/**`、`go.mod`/`go.sum` 等任务实际执行的内容）变更时运行。它检出 main 并固定当次提交，同一时刻只运行一个。

1. **prepare**：逐一核对 main 中原文与 provenance 一致，并获取官方当前脚本。
   - 下载器只允许 HTTPS→HTTPS 跳转（最多 5 次），拒绝 HTTP 降级、URL 凭据和私网/回环/metadata 地址；DNS 结果和实际 socket 对端都要检查。
   - 只使用系统 TLS 信任，不继承环境代理。
   - 任一脚本获取失败时，仍汇总报告所有脚本结果，然后任务失败。
2. **validate**：无变化时直接结束。有变化时在无网络、无凭据、无 capabilities、只读源码的容器（`.github/installer-check/Dockerfile`）里应用 patch 并运行 Shell 测试。容器不含 PowerShell。该镜像的 Ubuntu 基础镜像按摘要固定，构建时不 `--pull`；apt 软件包仍取自构建当时的发行版仓库。
3. **package**：回到可信主机重新应用 patch，比对容器结果，只打包允许的文件（声明的 `upstream/` 脚本、对应 `generated/` 文件和 `provenance.json`）。公钥、许可证、patch、descriptor、代码和 workflow 不在允许集合内。
4. **Windows 门禁**：复用 `.github/workflows/windows-installers.yml`，在 Windows Server 2022 上核对候选 ZIP 的摘要和文件集合，然后用 PowerShell 7 和 Windows PowerShell 5.1 解析并运行全部 ps1 的无害行为测试。该任务只有 `contents: read`。
5. **draft-pr**：只有 Linux 和 Windows 都成功后才执行。使用短期 `GITHUB_TOKEN`，每个应用各自创建草稿 PR，分支为 `automation/installer-updates/<vendor>/<app>/<内容摘要>`。

草稿 PR 的生命周期：

- 每个应用只保留最新一个草稿。内容相同不新建；内容变化时先建新草稿，再在旧草稿留言说明被哪个 PR 取代，关闭旧草稿并按旧 head 做租约检查后删除分支。
- 旧草稿含人工提交、已转为非草稿、非 bot 创建或含其他应用文件时停止，不覆盖人工工作。fork 中的同名分支忽略。
- 推送成功但 PR API 失败时，下次运行校验分支树和父提交一致后复用分支并补建 PR。
- 仓库需要开启 Settings → Actions → General 中的 “Allow GitHub Actions to create and approve pull requests”。工作流不修改仓库设置，也不自动合并；创建分支和删除被取代的分支都使用基于预期提交的 `--force-with-lease`，从不做无条件 force push。

## 测试入口

| 命令 | 覆盖 |
| --- | --- |
| `python3 scripts/test-installers.py --platform shell` | Linux：两种协议的 Shell 安装器、loopback HTTP、无害制品、摘要与失败路径 |
| `python3 scripts/test-update-installers.py` | 生成一致性、摘要/上下文失败时发布目录不变 |
| `python3 scripts/test-installer-maintenance.py` | 每日维护全流程的离线故障回归（下载边界、TLS、签名移除、patch 偏移/冲突、打包白名单、草稿 PR 幂等与保护） |
| `python scripts/test-installers.py --platform windows` | Windows：PS7 与 5.1 解析并执行所有声明的 ps1 |
| `python scripts/test-installer-candidates.py` | 普通 CI：候选 ZIP 入口，并证明坏候选不能被旧的好脚本掩盖 |

- `--application <vendor/app> --directory <generated-dir>` 可只测单个候选。每日更新使用 `--bundle <zip> --sha256 <digest> --baseline <commit>`，不能与单应用过滤混用。
- 所有 CLI 都是无害桩，不执行官方 Codex/Claude 二进制。Linux 上的 Darwin、musl、ARM64 和 Windows 上的 Claude ARM64 只是模拟平台探测，不代表实机验证。

## 修改安装器时的检查清单

- 不改 upstream；只改 patch，再用工具重新生成 generated。
- 重新确认 generated 与 patch 一致、正常安装成功、篡改/截断失败、失败关闭、交互语义不变。
- PowerShell 改动必须经过 Windows 门禁（PS7 和 5.1）。
- 许可材料变化时同步 `third_party/README.md`。
