# 构建依赖许可原文

以下原文随仓库交付，固定版本对应 `go.mod` / `go.sum` 与构建记录：

| 组件 | 版本 | 文件 |
| --- | --- | --- |
| Go 编译器和运行时 | 1.27.1 | [Go.LICENSE](Go.LICENSE) |
| github.com/mattn/go-sqlite3 | 1.14.32 | [go-sqlite3.LICENSE](go-sqlite3.LICENSE) |
| github.com/dustin/go-humanize | 1.1.0 | [go-humanize.LICENSE](go-humanize.LICENSE) |
| github.com/yuin/goldmark | 1.7.13 | [goldmark.LICENSE](goldmark.LICENSE) |
| Vue 前端运行时 | 3.5.43 | [vue.LICENSE](vue.LICENSE) |
| uPlot 图表运行时 | 1.6.32 | [uplot.LICENSE](uplot.LICENSE) |
| go.yaml.in/yaml/v3 | 3.0.5 | [go-yaml.LICENSE](go-yaml.LICENSE)、[go-yaml.NOTICE](go-yaml.NOTICE) |
| golang.org/x/crypto | 0.42.0 | [x-crypto.LICENSE](x-crypto.LICENSE) |

Codex 固定安装器的 LICENSE/NOTICE 在 `installers/openai/codex/upstream/`，同时嵌入服务并通过 `/openai/codex/licenses/LICENSE` 与 `/openai/codex/licenses/NOTICE` 提供。该目录不代表已完成六平台 Codex 二进制捆绑组件许可核查；对应门禁见验收记录。

OpenAI 品牌标志由用户提供，来源指向 Wikimedia Commons；原文、摘要和来源页许可/商标标签见 [模块素材说明](../internal/apps/codex/assets/README.md)。该标志不是 Codex 专属图标，也不属于 RedApp 原创代码的 MIT 授权范围。

Claude Code 官方安装器、公钥与官方仓库许可说明独立保存在 `installers/anthropic/claude-code/upstream/`，来源与摘要见该模块 `provenance.json`。其 [LICENSE.md](../installers/anthropic/claude-code/upstream/LICENSE.md) 声明 Anthropic 保留权利并适用商业条款，不属于 RedApp 原创代码的 MIT 授权。服务提供 `/anthropic/claude-code/LICENSE.md`。尚未取得/验证真实二进制；技术分发能力不等于公开再分发授权，额外再分发授权尚未核实，使用或继续分发第三方材料时须自行核对适用条款。

用户指定的 Anthropic 厂商与 Claude Code 应用 SVG 来自 Dashboard Icons，来源、摘要及使用边界见[素材说明](../internal/apps/builtin/assets/README.md)，上游仓库许可原文见 [dashboard-icons.LICENSE](dashboard-icons.LICENSE)。通过编译内置固定资源提供，不依赖运行时 CDN。
