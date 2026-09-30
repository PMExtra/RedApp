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
