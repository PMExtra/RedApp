# RedApp

[English](README.md)

RedApp 帮助 IT 管理员通过内网下载入口分发 **Codex CLI**。按需缓存下载，并提供管理页面查看和管理缓存版本、磁盘用量及流量。

## 快速上手

使用能够拉取镜像的 Linux/amd64 或 Linux/arm64（aarch64）主机，或[从源码构建](docs/README.md#build-from-source)。尚未验证 GHCR 匿名访问。当前镜像使用 `/var/lib/redapp`；旧 v0.1.0 镜像使用 `/data`，不能搭配下面的卷路径。

```sh
docker run -d --name redapp --read-only \
  -p 127.0.0.1:8080:8080 -v redapp-data:/var/lib/redapp \
  -e REDAPP_PUBLIC_URL=http://localhost:8080 \
  ghcr.io/pmextra/redapp:latest
docker logs redapp
```

打开 **http://localhost:8080/** 浏览应用及 Codex 安装说明。管理员在 **http://localhost:8080/admin/** 登录。初始管理员密码仅出现在首次启动日志中；登录后请修改密码并保护日志。命名卷会在更换容器后保留数据。

企业访问应通过 HTTPS 反向代理。`REDAPP_PUBLIC_URL` 可选：显式设置可固定对外 origin，否则按请求推导。使用转发头前配置可信代理 CIDR，并通过代理或网络策略限制访问。详见[部署与配置](docs/operations.md)。

Docker 自动选择 Linux/amd64 或 Linux/arm64（aarch64）。需要固定服务版本时，可使用 `v0.4.1` 等版本标签。

## 客户端安装

将 `codex.example.internal` 替换为服务地址：

```sh
curl -fsSL https://codex.example.internal/install.sh | sh
```

```powershell
irm 'https://codex.example.internal/install.ps1' | iex
```

上述命令会立即下载并执行安装器，使用已设置的 `CODEX_RELEASE`，否则使用 `latest`。指定版本的命令见安装详解。无人值守 shell 安装时，将 `CODEX_NON_INTERACTIVE=1` 放在管道中 `sh` 的前面。如需先审查脚本，见[客户端安装详解](docs/README.md#review-before-installing)。

## 上线前确认

- 每个本地数据目录只运行一个服务实例；不支持共享网络文件系统和 public URL 子路径。
- 安装器下载仅走本服务。Codex 运行期/API 流量不会被改写，抑制安装器更新标记也不会禁用所有 CLI 更新检查；请实施企业出口策略。
- Windows/macOS 安装和生产上游下载链仍需上线验证。

高级配置、备份、设计、验收及开发说明见[文档导航](docs/README.md)。详细参考文档目前使用中文。

## 许可

RedApp 原创代码采用 [MIT 许可](LICENSE)。[第三方许可](third_party/README.md)（包括 Codex 安装器 LICENSE/NOTICE）独立保留。
