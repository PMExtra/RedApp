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
