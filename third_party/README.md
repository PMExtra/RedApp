# 构建依赖许可原文

以下原文随仓库交付，固定版本对应 `go.mod` / `go.sum` 与构建记录：

| 组件 | 版本 | 文件 |
| --- | --- | --- |
| Go 编译器和运行时 | 1.25.1 | [Go.LICENSE](Go.LICENSE) |
| github.com/mattn/go-sqlite3 | 1.14.32 | [go-sqlite3.LICENSE](go-sqlite3.LICENSE) |
| golang.org/x/crypto | 0.42.0 | [x-crypto.LICENSE](x-crypto.LICENSE) |

Codex 固定安装器的 LICENSE/NOTICE 在 `installers/codex/upstream/`，同时嵌入服务并通过 `/licenses/LICENSE` 与 `/licenses/NOTICE` 提供。该目录不代表已完成六平台 Codex 二进制捆绑组件许可核查；对应门禁见验收记录。
