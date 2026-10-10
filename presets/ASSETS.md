# 预置展示图片来源

`presets/assets/` 只保存经审查的静态图片，作为构建嵌入资源；无运行时下载。以下均逐字节复制现有文件，保留原文件与许可/来源记录，视觉不变。

| 预置路径 | 原始文件及来源记录 |
| --- | --- |
| `assets/builtin/openai.svg` | `internal/apps/codex/assets/openai-symbol.svg`；[来源记录](../internal/apps/codex/assets/README.md) |
| `assets/openai/codex/icon.svg` | 同上 |
| `assets/builtin/anthropic.svg` | `internal/apps/builtin/assets/anthropic-light.svg`；[来源及许可记录](../internal/apps/builtin/assets/README.md) |
| `assets/anthropic/claude-code/icon.svg` | `internal/apps/builtin/assets/anthropic.svg`；同上，Apache-2.0 原文仍在 `third_party/dashboard-icons.LICENSE` |

新增资源须审查来源与许可。加载器校验全部图片和引用，但不以具体厂商名称列白名单。展示资源与安装器协议资源独立；新 inventory 的 icon 为展示 URL，其他安装器维护字段与白名单保持原值。语义 hash 按规范 spec 计算，可信分发摘要单独计算。
