# Codex CLI 上游分发契约核验

- 核验时间：2026-09-30 20:43–20:46 UTC
- 范围：只读检查官方元数据、源码及小型 checksum 文本；未执行安装器、下载二进制包、修改 GitHub 或实现分发服务
- 关联设计：[design.md](design.md)
- 本文分开记录“已观察事实”和“本项目实现约束”；不是上游兼容性保证或法律意见

## 1. 结论与重要修正

1. `channels/latest` 当前返回完整 release 对象，而不是只有版本号或一个 release.json URL。服务应从其 `tag_name` 解析版本，并验证资产索引；可以再获取固定版本清单，但不能假定 latest 是纯文本。
2. 两种安装器优先选择完整 `codex-package-{target}.tar.gz`，同时需要 `codex-package_SHA256SUMS`；不是只下载一个裸 codex 二进制。
3. 包路径的客户端信任链是：HTTPS 元数据中的 manifest SHA-256 → manifest 原始字节 → manifest 中包名对应的 archive SHA-256 → 解包。legacy platform-npm 路径直接使用资产元数据中的 archive SHA-256。
4. 只改 releases base URL 不足以保证客户端只访问企业服务：脚本有 GitHub 元数据、资产及校验失败后的重新解析回退。必须封闭全部出口，并覆盖失败路径。
5. 已固定可重复获取的仓库/版本发布脚本基线，且源码字节摘要与 0.159.2 发布资产元数据相符；没有证明可变根路径 install.sh/install.ps1 与此基线逐字节相同。
6. 官方脚本有 latest 自动更新标记和可选启动/旧包管理器卸载行为。企业安装器的网络边界不能只审核 curl/Invoke-WebRequest，还要明确这些行为的范围。

## 2. 固定来源与身份

### 2.1 仓库和版本身份

- 官方仓库：[openai/codex](https://github.com/openai/codex)
- 本次 main 解析为 [60947e234156ac12bdb7fba2477d3965f166bd34](https://github.com/openai/codex/commit/60947e234156ac12bdb7fba2477d3965f166bd34)
- [rust-v0.159.2 Git ref](https://api.github.com/repos/openai/codex/git/ref/tags/rust-v0.159.2) 指向 annotated tag object `8b9fa496bbf2c47aebd62e85a080b9a522a455b5`
- [tag object](https://api.github.com/repos/openai/codex/git/tags/8b9fa496bbf2c47aebd62e85a080b9a522a455b5) 指向 commit `ff6aec96948b70d94983af2641a6b67c94faeff5`；API 报告该 tag 为 unsigned，不能将固定 commit 与已验证签名混为一谈
- [GitHub release 元数据](https://api.github.com/repos/openai/codex/releases/tags/rust-v0.159.2)：release ID `399607589`，`draft=false`、`prerelease=false`，发布时间 `2026-09-29T23:57:16Z`。其 `target_commitish=main` 不是固定提交，应使用上述 tag 解引用结果

建议实现初始基线使用发布 commit `ff6aec96948b70d94983af2641a6b67c94faeff5`，而非可变 main。

### 2.2 脚本摘要

通过 GitHub 文件接口获取 UTF-8 内容，在本地恢复精确原始字节后，同时计算 Git blob SHA-1 和 SHA-256；blob SHA 与接口返回值相同。两个 commit 下的相应 installer Git blobs 相同。

| 文件 | 原始字节数 | Git blob SHA-1 | 文件 SHA-256 |
| --- | ---: | --- | --- |
| install.sh | 34564 | 58815c797ecb02eb738f3bc77e0279e3328572a4 | 150e3cf675682efeaac115aa3747add3f27887896d04ce6d0b56478d8b428bf6 |
| install.ps1 | 43798 | cd9c0c778916362fc0e41b522bd804f6ce6e24ad | 3522b77d4485eac014e70fa946787c95fad3874a4e9047557c1e044eb268d13e |

固定文件：[install.sh](https://github.com/openai/codex/blob/ff6aec96948b70d94983af2641a6b67c94faeff5/scripts/install/install.sh)、[install.ps1](https://github.com/openai/codex/blob/ff6aec96948b70d94983af2641a6b67c94faeff5/scripts/install/install.ps1)。

两项 SHA-256 与字节数均匹配 GitHub 0.159.2 release 的同名资产条目。此检查是“源码字节与发布清单承诺一致”，不是对下载到的发布资产字节进行了独立验证。

### 2.3 可变线上脚本的边界

- [线上 install.ps1](https://releases.openai.com/codex/install.ps1) 已通过网页文本读取确认包选择及双层哈希流程；网页提取不是原始字节证据，不能用它计算可靠文件摘要
- [线上 install.sh](https://releases.openai.com/codex/install.sh) 未取得可用于摘要比较的原始字节，不能证明与固定仓库基线逐字节相同
- 本文所有精确函数/源码分析以固定仓库文件为准，不能表述为“已证明当前线上脚本完全等同”
- 未来更新命令若选择线上根路径作为来源，必须获取原始字节并保存 SHA-256、获取时间、最终 URL、响应头及与仓库基线的比较结果

## 3. 元数据解析契约

官方固定根为 `https://releases.openai.com/codex`：

- latest：`/channels/latest`
- 指定版本：`/releases/{normalizedVersion}/release.json`
- 制品：`/releases/{normalizedVersion}/{assetName}`

[当前 latest](https://releases.openai.com/codex/channels/latest) 和 [0.159.2 清单](https://releases.openai.com/codex/releases/0.159.2/release.json) 本次均显示 `tag_name=rust-v0.159.2`，资产项包含 `name`、`digest`、`browser_download_url`。已观察 URL 为绝对 HTTPS URL；OpenAI 清单未显示 size。不要依赖 size 必填，也不要把提取结果视为原始响应字节快照。

### 3.1 本项目应固定的解析规则

- 使用标准 JSON 解析器，限制响应体大小、资产数、字段长度；拒绝重复关键字段和冲突的重复资产名
- `tag_name` 必须与请求版本一致；未知版本只能通过固定的官方版本路由发现
- asset digest 支持且仅支持明确配置的算法；首版为 `sha256:` 加恰好 64 个十六进制字符，内部规范化为小写
- 禁止凭文件名模式自行授权不存在于可信清单中的文件
- 保存原始元数据与解析索引；企业改写响应仅替换下载 URL，不替换 name/digest，不修改 checksum manifest 字节
- size 若无则为未知，不用零字节代替；GitHub size 不应未经身份核对便移植到 OpenAI origin
- 绝对 URL 的 origin、路径、版本和资产名均须校验；未观察相对 URL，不默认支持；未来需要支持时必须以可信固定 base 解析并再次校验
- 不把所有 GitHub release 元数据额外字段设为必填；OpenAI 的精简结构与 GitHub API 全对象不同
- 所有 SHA-256 是完整压缩资产字节的哈希，不是解压目录哈希、ETag、Git blob ID 或 HTTP chunk 哈希

### 3.2 版本格式

固定 shell 的 `normalize_version` 与 PowerShell `Normalize-Version` 接受空值/latest、去掉 `rust-v` 或 `v` 前缀。两个安装器验证器接受：

`x.y.z`、`x.y.z-alpha`、`x.y.z-alpha.N`、`x.y.z-alpha.N.M`、`x.y.z-beta`、`x.y.z-beta.N`。

它们不接受通用任意 SemVer 后缀，例如 `-rc.1` 或 build metadata。设计中的 rc 清理示例是排序示意，不能作为当前官方安装输入支持声明。服务可以保留更一般的安全 SemVer 排序，但未知/不能解释的版本必须保留并报告，不能猜测清理。尚未验证真实历史预发行版资产完备性。

## 4. 平台选择与安装制品

依据固定脚本的 `select_release_assets` / `Resolve-ReleaseAssetSelection`：

| 平台 | target | legacy npm tag |
| --- | --- | --- |
| macOS Apple Silicon | aarch64-apple-darwin | darwin-arm64 |
| macOS Intel | x86_64-apple-darwin | darwin-x64 |
| Linux ARM64 | aarch64-unknown-linux-musl | linux-arm64 |
| Linux x64 | x86_64-unknown-linux-musl | linux-x64 |
| Windows ARM64 | aarch64-pc-windows-msvc | win32-arm64 |
| Windows x64 | x86_64-pc-windows-msvc | win32-x64 |

- shell 根据 uname 检测架构；macOS 在 x86_64 进程中检查 Rosetta，必要时改选 aarch64
- Linux 安装器选择 musl，不动态改选 GNU/glibc 包
- PowerShell 要求 Windows 64 位，按 OSArchitecture 而非仅当前进程架构选择
- 首选 `codex-package-{target}.tar.gz` 且需同时有 `codex-package_SHA256SUMS`
- 否则寻找 `codex-npm-{npmTag}-{version}.tgz`，没有则失败；这是从 release 下载 tgz，不是执行 npm install
- 不选裸二进制、.zst、.zip、.dmg；这些可能出现在清单，但不代表该安装路径使用它们
- daemon-only 改变本地安装目录与选择逻辑；当前资产选择函数仍使用 codex-package，而非仅凭该标志选择 codex-app-server-package

### 4.1 0.159.2 小型校验样本与尺寸

下表来自 [GitHub 0.159.2 release API](https://api.github.com/repos/openai/codex/releases/tags/rust-v0.159.2)；SHA-256 去掉前缀。未下载这些 archive。

| 资产 | size / bytes | SHA-256 |
| --- | ---: | --- |
| codex-package-aarch64-apple-darwin.tar.gz | 129522674 | 38aaf6dce63099fd10988948d03bbc6c0474253aef6961fcbe60f8d154b39101 |
| codex-package-x86_64-apple-darwin.tar.gz | 140813958 | 6b9b38bfad6ac8019aa6a243ee3ab11d3e22889eafd5458b0344cf20e797e680 |
| codex-package-aarch64-unknown-linux-musl.tar.gz | 150270060 | 05a524a463cadf7e3e22c7f923539c0d0b74c3e78b1f5f1fab52e50e6fb3312f |
| codex-package-x86_64-unknown-linux-musl.tar.gz | 159961162 | 9e2d29a713b94478b240dec2f10e11324cd05fad76dc43e7c639bdf8a1337a6b |
| codex-package-aarch64-pc-windows-msvc.tar.gz | 144725539 | f017342f77ec57dffff57e59723738ac9f228ddab4f656c2bdfc65f1d9b87da3 |
| codex-package-x86_64-pc-windows-msvc.tar.gz | 156614559 | a7ab591043d99e8a88d0c2bebf15f810b15599a71148c4af944a755af3c71eef |
| codex-package_SHA256SUMS | 1631 | e9eced4d7a65c0b6d9bfdf52922d3079ce9ecf7c7ae3c345bbd75e27d795af97 |

[OpenAI checksum manifest](https://releases.openai.com/codex/releases/0.159.2/codex-package_SHA256SUMS) 的对应六个包条目与上表一致。其内容还含 app-server/provisioned 包；不能因此自动扩大安装器范围。

## 5. 哈希与回退的准确语义

### 5.1 正常流程

shell：`release_asset_digest` → `download_file_with_fallback` 下载并验证 manifest → `package_archive_digest` 找包哈希 → 下载并验证 archive → `install_package_release` 解包。

PowerShell：`Find-ReleaseAssetMetadata` → `Invoke-WebRequestWithFallback` 验证 manifest → `Get-PackageArchiveDigest` → 再次 `Invoke-WebRequestWithFallback` 验证 archive → tar 解包。

manifest 行为十六进制摘要、空白、精确资产名。两种解析器细节不同；项目生成/保留标准格式即可，不应认为任意 sha256sum 文本格式均可兼容（如文件名前的星号）。包主路径的客户端 archive 期望值来自已验证 manifest；服务端仍按 release.json 的资产 digest 验证。本项目应测试两者矛盾时失败，不能重写摘要来“修复”。

legacy tgz 路径省略 manifest，直接验证 release 资产摘要。两条路径都在哈希通过后才解包。已安装完整版本可能被复用，不代表每次运行都会重新下载校验。

### 5.2 原版脚本的外部回退

默认 `CODEX_INSTALLER_USE_RELEASES_OPENAI_COM` 为 true；用户可设 0/false/no 改用 GitHub。OpenAI 元数据获取/解析/目标资产选择失败，也会落入 GitHub：

- latest：`https://api.github.com/repos/openai/codex/releases/latest`
- 固定版本：`https://api.github.com/repos/openai/codex/releases/tags/rust-v{version}`
- 固定资产：`https://github.com/openai/codex/releases/download/rust-v{version}/{asset}`

shell 构造资产 URL，不读取 `browser_download_url`；PowerShell 在 OpenAI 模式覆盖构造 URL，在 GitHub 模式可读取该字段。仅重写 JSON 中 URL 因而不足以覆盖 shell。

下载或校验失败后，原版尝试 GitHub fallback URL，先用原预期哈希；仍不匹配则重新读取 GitHub 对应版本元数据并按 GitHub 资产哈希验证。若对象是 manifest，还要求其中能找到待安装包。它不等同于无条件跳过哈希，但会切换信任输入，不能直接照搬进“release.json 固定身份”的企业缓存。

本项目建议客户端只走企业根地址并 fail closed，服务器是否采用第二官方 origin 属于独立严格策略：不得以同名文件掩盖不同哈希，不得悄悄改变活动 generation 预期哈希。首版可以不实现服务器 GitHub 回退而返回可诊断错误；若实现，必须记录来源及绑定一致的资产身份。

## 6. 安装器维护/更新命令的变换点

这是后续实现清单，本文未创建 patch 或更新器。

1. **固定基线**：按不可变 commit 获取两个原文件、LICENSE、NOTICE，保留原字节、Git blob、SHA-256、取得时间；禁止把随时变化的 main 当固定基线
2. **根地址**：变换 shell `RELEASES_BASE_URL`、PowerShell `$ReleasesBaseUri` 为服务配置生成的可信 public base URL；正确转义 shell/PowerShell 字面量，拒绝换行/命令注入，不直接采用请求 Host
3. **元数据入口**：修改 `resolve_release` / `Resolve-Release` 与 GitHub resolver，使偏好开关和异常路径均无法向公网回退
4. **资产入口**：修改 `release_url_for_asset`、`select_release_assets`、`Resolve-ReleaseAssetSelection` 的 fallback 构造；清空回退 URL 或路由到企业服务中明确批准的专用入口，不能保留公共地址
5. **失败路径**：修改 `download_file_with_fallback` / `Invoke-WebRequestWithFallback`，移除客户端 GitHub 重取 digest；保留下载不完整、manifest 缺项和哈希不符均失败的语义
6. **企业元数据**：URL 改写只用于企业自有响应；校验文本和二进制按原字节返回，元数据需要的完整资产集合必须包含 checksum manifest
7. **非交互/子进程**：官方支持非交互并拒绝交互问询。交互模式可能运行 brew/npm/bun 卸载旧安装，或启动 Codex；这些子进程不能被“替换下载 base URL”约束。本项目保留官方交互语义；无人值守安装显式启用非交互，并通过企业出口控制约束子进程
8. **运行时自动更新边界**：安装 latest 会写 `packages/standalone/auto-update-version`，指定版本会移除该标记；daemon 路径还有对应目录/守卫。R19 的本地更新命令只维护企业安装脚本，不等于改写 CLI 内置更新器。若目标还要求 CLI 运行期所有网络均内网，需要另行确定范围，不能声称本任务已完成
9. **修改声明**：修改后的脚本增加醒目变更说明与基线身份；不得把企业改版冒充官方原文件
10. **失效关闭**：更新器下载到临时目录，校验源身份，严格应用 patch；上下文变化时失败退出，不模糊匹配、不忽略 hunk、不覆盖已发布 generated 版本。审核 URL/网络调用/哈希路径差异并通过测试后再原子发布

### 6.1 必测网络和完整性矩阵

- 两种脚本、六个目标平台、latest/固定版本、当前包/legacy tgz
- 元数据超时、非 JSON、错误 tag、缺 digest、缺包、缺 manifest；公网 fallback 不得触发
- manifest 哈希错误、缺包行、包哈希错误、截断下载；不得完成安装
- 显式设置原有 GitHub 偏好环境变量，仍不得产生公共下载请求
- 公网 URL 被注入 `browser_download_url`、服务重定向到未授权 origin；客户端和服务器对应防线均须测试
- 流式验证失败必须阻止安装；不能只验证服务端日志
- 源码变更导致 patch 失败时保留旧发布脚本；生成结果中扫描所有 HTTP URL 与网络调用，再用可控假下载器记录实际请求
- Windows PowerShell 与 PowerShell 7、macOS/Linux shell 的实际执行差异需要对应平台测试，不以静态阅读替代

## 7. 许可证和 NOTICE：事实与发布检查

本段是许可证原文及源码观察的摘要，不提供法律意见，也不声明第三方二进制合规审计已完成。

[固定 LICENSE](https://github.com/openai/codex/blob/60947e234156ac12bdb7fba2477d3965f166bd34/LICENSE) 为 Apache License 2.0。其第 4 条要求分发时向接收者提供许可证、让修改文件携带醒目修改声明、保留适用的源代码版权/专利/商标/归属说明，并按条件保留 NOTICE 的可读归属内容。第 6 条不提供一般性的商标使用许可。

[固定 NOTICE](https://github.com/openai/codex/blob/60947e234156ac12bdb7fba2477d3965f166bd34/NOTICE) 包含 OpenAI Codex / Copyright 2025 OpenAI，并说明项目含源自 MIT 许可 Ratatui 的代码及 Florian Dehau、Ratatui Developers 的版权归属。更新脚本基线时应同步审查，不自行删掉。

源码材料说明完整包可能包含 rg、bwrap、zsh 及 Windows helpers；[package layout 实现](https://github.com/openai/codex/blob/60947e234156ac12bdb7fba2477d3965f166bd34/scripts/codex_package/layout.py) 和 [builder README](https://github.com/openai/codex/blob/60947e234156ac12bdb7fba2477d3965f166bd34/scripts/codex_package/README.md) 支持这一点。仅查仓库根 Apache LICENSE 无法证明所有捆绑组件只有该许可。

本次没有打开实际 archive，也没有穷举其第三方许可材料；不能声称发布包已经自带完整 LICENSE/NOTICE 或无需额外材料。正式企业分发前，应核查代表性六平台包的文件清单及随包许可、第三方声明和必要源代码获取说明；相关法律判断交由适当审阅人员。为保持官方哈希，不能在官方压缩包中追加许可证后继续使用原摘要；可随服务/安装器交付独立的许可材料，并明确关联版本。

## 8. 未完成核验与实现门禁

| 项目 | 当前证据/状态 | 后续验收 |
| --- | --- | --- |
| 固定仓库脚本 | commit/blob/SHA-256/size 已核对，匹配 release 元数据 | 在实现库保存原始基线与来源记录 |
| 线上根脚本身份 | ps1 文本已读；两种线上根脚本均未完成原始字节比对 | 若以线上根脚本作为来源，获取原字节并比对；否则明确采用固定发布基线 |
| 元数据 | 当前对象字段、版本、哈希语义已观察 | 原始响应 fixture；重复字段/坏数据/未知字段测试 |
| checksum manifest | 小型文本已读，六包摘要与 GitHub 元数据匹配 | 获取原字节并验证其自身 digest；测试 parser 和 archive 矛盾场景 |
| 历史/预发行版本 | 脚本显示固定版本查询方式与受限格式 | 至少一例真实旧 tgz 和一例 alpha/beta fixture；不能承诺全部历史版本 |
| 真实 Range/ETag/重定向 | 未向大型资产做下载或 Range/HEAD 探测 | 可选受限线上集成探测；本地 HTTP 测试验证 200/206/416/validator/编码全部分支 |
| CDN allowlist | 已知根 origin 和脚本 URL；未观测真实每跳链 | 依据实际链建立最小允许列表，不猜测泛域名 |
| 平台运行 | 仅静态检查 | 三类 OS 安装/失败路径实测，独立网络记录 |
| 许可 | 根 LICENSE/NOTICE 已核实；组件/包内容未完整核查 | 分发前检查实际包与组件所需材料 |
| 内置自动更新 | 仅证实安装脚本写入/删除 marker | 不扩展为 CLI updater 支持声明；若需求改变先确认范围 |

核心服务单元测试、并发/恢复/清理测试不应依赖生产上游。元数据读取得到的哈希也不是发布者签名：目前信任根仍是受控 HTTPS 来源与保存的官方清单。
