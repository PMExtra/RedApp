# RedApp

[English](README.md)

RedApp 是自托管的软件下载再分发服务。它在内网用同一地址分发 HTTP 文件、Codex CLI 和 Claude Code，把上游文件缓存在本地磁盘，并生成指向本服务的安装命令。管理员在 Web 后台管理厂商和应用；用户浏览公开目录并执行安装命令。

## 功能

- 五种 Provider：应用信息页、托管上传文件、通用 HTTP 缓存，以及经过校验的 Codex 和 Claude Code 发布。
- HTTP 缓存支持有序路径规则、旧缓存回退、1–16 个镜像源、刷新以及手动或自动清理。
- Codex 和 Claude Code 的发布元数据与二进制按上游摘要和签名校验。
- 官方安装器仅做最小修改，改为从本 RedApp 服务下载。
- 中英双语的公开目录、应用页面和使用说明。
- 内置预置模板，支持逐字段覆盖、配置导出/导入和应用复制。
- 按作用域（全局、厂商、应用）配置出口代理，支持预热和版本保留。
- 磁盘、流量、请求和资源的指标历史。

## 快速开始

Docker（只读根文件系统，数据位于命名卷）：

```sh
docker build -t redapp:local .
docker run -d --name redapp --read-only \
  -p 127.0.0.1:8080:8080 \
  -v redapp-data:/var/lib/redapp \
  redapp:local
```

原生二进制（Linux，需要 Go、Node.js 和 C 工具链）：

```sh
make build
./bin/redapp serve --data /absolute/writable/redapp-data
```

无需配置文件，仅用环境变量即可启动：

```sh
REDAPP_DATA=/absolute/writable/redapp-data \
REDAPP_LISTEN=127.0.0.1:8080 \
REDAPP_PUBLIC_URL=https://downloads.example.internal \
  redapp
```

## 首次登录

1. 首次启动后查看服务日志，其中只打印一次 `Initial admin password: ...`。
2. 打开 `http://localhost:8080/admin/overview`，以管理员身份登录。
3. 修改密码，然后保护或轮转包含初始密码的日志。
4. 内置厂商和应用初始为禁用状态。同时启用厂商及其应用后才会公开。

公开目录位于 `http://localhost:8080/`。

## 安装客户端

将 `downloads.example.internal` 替换为你的公共地址。每个应用页面都会显示针对本服务生成的相同命令。

```sh
curl -fsSL https://downloads.example.internal/openai/codex/install.sh | sh
curl -fsSL https://downloads.example.internal/anthropic/claude-code/install.sh | bash
```

```powershell
irm 'https://downloads.example.internal/openai/codex/install.ps1' | iex
irm 'https://downloads.example.internal/anthropic/claude-code/install.ps1' | iex
```

这些命令会执行下载的脚本。如果你的安全策略有要求，请先审查脚本。

## 文档

- [配置](docs/guide/configuration.zh-CN.md)：部署设置、公共地址、代理、预置模板、导出和导入。
- [运维](docs/guide/operations.zh-CN.md)：Docker、数据目录、反向代理、健康检查、备份和故障排查。
- [Provider](docs/guide/providers.zh-CN.md)：Provider 类型、HTTP 缓存规则、发布分发和使用说明。
- [安全](docs/guide/security.zh-CN.md)：信任模型、认证、沙箱和上游校验。
- [指标](docs/guide/metrics.zh-CN.md)：指标名称、采样、保留和缺失数据。
- [开发](docs/dev/development.md)：构建、测试和参与开发（中文）。

## 许可

RedApp 采用 [MIT 许可](LICENSE)。第三方组件和上游安装器保留各自的许可，见 [third_party](third_party/README.md)。
