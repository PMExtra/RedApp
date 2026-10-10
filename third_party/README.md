# 构建依赖许可原文

以下原文随仓库交付，固定版本对应 `go.mod` / `go.sum` 与构建记录：

| 组件 | 版本 | 文件 |
| --- | --- | --- |
| Go 编译器和运行时 | 1.27.1 | [Go.LICENSE](Go.LICENSE) |
| github.com/mattn/go-sqlite3 | 1.14.32 | [go-sqlite3.LICENSE](go-sqlite3.LICENSE) |
| github.com/dustin/go-humanize | 1.1.0 | [go-humanize.LICENSE](go-humanize.LICENSE) |
| github.com/yuin/goldmark | 1.7.13 | [goldmark.LICENSE](goldmark.LICENSE) |
| go.yaml.in/yaml/v3 | 3.0.5 | [go-yaml.LICENSE](go-yaml.LICENSE)、[go-yaml.NOTICE](go-yaml.NOTICE) |
| golang.org/x/net（HTML 目录解析） | 0.44.0 | [x-net.LICENSE](x-net.LICENSE)、[x-net.PATENTS](x-net.PATENTS) |
| github.com/bmatcuk/doublestar/v4 | 4.10.2 | [doublestar.LICENSE](doublestar.LICENSE) |
| golang.org/x/crypto（bcrypt） | 0.42.0 | [x-crypto.LICENSE](x-crypto.LICENSE) |
| github.com/ProtonMail/go-crypto（Claude Code 清单 OpenPGP 验签） | 1.5.2 | [protonmail-go-crypto.LICENSE](protonmail-go-crypto.LICENSE)、[protonmail-go-crypto.PATENTS](protonmail-go-crypto.PATENTS) |
| github.com/cloudflare/circl（go-crypto 的间接依赖） | 1.6.3 | [circl.LICENSE](circl.LICENSE) |
| golang.org/x/sys（circl 的间接依赖） | 0.36.0 | [x-sys.LICENSE](x-sys.LICENSE)、[x-sys.PATENTS](x-sys.PATENTS) |
| golang.org/x/text（Tag Unicode 规范化与大小写折叠） | 0.42.0 | [x-text.LICENSE](x-text.LICENSE)、[x-text.PATENTS](x-text.PATENTS) |
| github.com/santhosh-tekuri/jsonschema/v6（仅测试：按 `api/openapi.yaml` 校验 HTTP 响应的 JSON Schema 2020-12 校验器，不编入二进制） | 6.0.3 | [jsonschema.LICENSE](jsonschema.LICENSE) |

Codex 固定安装器的 LICENSE/NOTICE 在 `installers/openai/codex/upstream/`，同时嵌入服务并通过 `/openai/codex/licenses/LICENSE` 与 `/openai/codex/licenses/NOTICE` 提供。该目录不代表已完成六平台 Codex 二进制捆绑组件的许可核查；正式企业分发前须另行核查实际包内的许可材料。

OpenAI 品牌标志由用户提供，来源指向 Wikimedia Commons；原文、摘要和来源页许可/商标标签见 [模块素材说明](../internal/apps/codex/assets/README.md)。该标志不是 Codex 专属图标，也不属于 RedApp 原创代码的 MIT 授权范围。

Claude Code 官方安装器、公钥与官方仓库许可说明独立保存在 `installers/anthropic/claude-code/upstream/`，来源与摘要见该模块 `provenance.json`。其 [LICENSE.md](../installers/anthropic/claude-code/upstream/LICENSE.md) 声明 Anthropic 保留权利并适用商业条款，不属于 RedApp 原创代码的 MIT 授权。服务提供 `/anthropic/claude-code/LICENSE.md`。尚未取得/验证真实二进制；技术分发能力不等于公开再分发授权，额外再分发授权尚未核实，使用或继续分发第三方材料时须自行核对适用条款。

用户指定的 Anthropic 厂商与 Claude Code 应用 SVG 来自 Dashboard Icons，来源、摘要及使用边界见[素材说明](../internal/apps/builtin/assets/README.md)，上游仓库许可原文见 [dashboard-icons.LICENSE](dashboard-icons.LICENSE)。通过编译内置固定资源提供，不依赖运行时 CDN。

## 前端产物中的 npm 包

以下包被打进 `internal/httpserver/web` 并随二进制分发，版本固定在 `frontend/package-lock.json`。`vite build` 会对照本表（按反引号中的包名）检查实际打包的每个 npm 包，缺失即构建失败；只在构建和测试时使用的工具（Vite、TypeScript、ESLint、Vitest、Playwright 等）不随产物分发，不在此列。

| 组件 | 包 | 版本 | 许可 | 文件 |
| --- | --- | --- | --- | --- |
| Vue 运行时 | `vue`、`@vue/runtime-dom`、`@vue/runtime-core`、`@vue/reactivity`、`@vue/shared` | 3.5.43 | MIT | [vue.LICENSE](vue.LICENSE) |
| Vue Router | `vue-router` | 5.4.0 | MIT | [vue-router.LICENSE](vue-router.LICENSE) |
| Pinia 客户端状态 | `pinia` | 4.0.3 | MIT | [pinia.LICENSE](pinia.LICENSE) |
| TanStack Query 服务端状态 | `@tanstack/vue-query`、`@tanstack/query-core` | 5.104.1 | MIT | [tanstack-query.LICENSE](tanstack-query.LICENSE) |
| vue-i18n 国际化 | `vue-i18n`、`@intlify/core-base`、`@intlify/message-compiler`、`@intlify/shared` | 11.4.13 | MIT | [vue-i18n.LICENSE](vue-i18n.LICENSE) |
| Reka UI 无样式组件 | `reka-ui` | 2.11.0 | MIT | [reka-ui.LICENSE](reka-ui.LICENSE) |
| Floating UI（Reka UI 浮层定位） | `@floating-ui/vue` | 2.0.1 | MIT | [floating-ui-vue.LICENSE](floating-ui-vue.LICENSE) |
| | `@floating-ui/dom`、`@floating-ui/core`、`@floating-ui/utils` | 1.8.0 / 1.8.0 / 0.2.12 | MIT | [floating-ui.LICENSE](floating-ui.LICENSE) |
| VueUse（Reka UI 依赖） | `@vueuse/core`、`@vueuse/shared` | 14.4.0 | MIT | [vueuse.LICENSE](vueuse.LICENSE) |
| aria-hidden（Reka UI 依赖） | `aria-hidden` | 1.2.6 | MIT | [aria-hidden.LICENSE](aria-hidden.LICENSE) |
| defu、ohash（Reka UI 依赖） | `defu`、`ohash` | 6.1.7 / 2.0.12 | MIT | [unjs-defu-ohash.LICENSE](unjs-defu-ohash.LICENSE) |
| Internationalized Number（Reka UI 数字输入依赖） | `@internationalized/number` | 3.6.9 | Apache-2.0 | [internationalized-number.LICENSE](internationalized-number.LICENSE) |
| openapi-fetch 类型化 API 客户端 | `openapi-fetch` | 0.17.0 | MIT | [openapi-fetch.LICENSE](openapi-fetch.LICENSE) |
| vee-validate 表单 | `vee-validate` | 4.15.1 | MIT | [vee-validate.LICENSE](vee-validate.LICENSE) |
| zod 校验 | `zod` | 4.6.5 | MIT | [zod.LICENSE](zod.LICENSE) |
| Lucide Vue UI 图标 | `@lucide/vue` | 1.55.0 | ISC | [lucide.LICENSE](lucide.LICENSE) |
| uPlot 图表运行时（指标历史图表） | `uplot` | 1.6.32 | MIT | [uplot.LICENSE](uplot.LICENSE) |
