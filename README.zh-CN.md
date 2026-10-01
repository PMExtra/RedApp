# RedApp

[English](README.md)

RedApp 帮助 IT 管理员通过内网下载入口分发 **Codex CLI**。按需缓存下载，并提供管理页面查看和管理缓存版本、磁盘用量及流量。

## 快速上手

使用能够拉取 v0.2.0 镜像的 Linux/amd64 主机，或[从源码构建](docs/README.md#build-from-source)。尚未验证 GHCR 匿名访问。v0.2.0 使用 `/var/lib/redapp`；旧 v0.1.0 镜像使用 `/data`，不能搭配下面的卷路径。

```sh
docker run -d --name redapp --read-only \
  -p 127.0.0.1:8080:8080 -v redapp-data:/var/lib/redapp \
  -e REDAPP_PUBLIC_URL=http://localhost:8080 \
  ghcr.io/pmextra/redapp:v0.2.0
docker logs redapp
```

打开 **http://localhost:8080/admin/**。初始管理员密码仅出现在首次启动日志中；登录后请修改密码并保护日志。命名卷会在更换容器后保留数据。

企业访问应通过 HTTPS 反向代理，并将 `REDAPP_PUBLIC_URL` 设置为对外 origin。代理须使用配置的 Host，并通过代理或网络策略限制下载访问。详见[部署与配置](docs/operations.md)。

当前源码支持构建 Linux/amd64 和 Linux/arm64（aarch64）；已发布 v0.2.0 镜像仅支持 amd64，多架构版本尚待发布。Docker 会自动选择主机架构。

## 客户端安装

将 `codex.example.internal` 替换为服务地址：

```sh
curl -fsS https://codex.example.internal/install.sh | sh -s -- --release 0.159.2
```

```powershell
& ([scriptblock]::Create((Invoke-WebRequest -UseBasicParsing 'https://codex.example.internal/install.ps1' -ErrorAction Stop).Content)) -Release 0.159.2
```

上述命令会立即下载并执行安装器。无人值守 shell 安装时，将 `CODEX_NON_INTERACTIVE=1` 放在管道中 `sh` 的前面。如需先审查脚本，见[客户端安装详解](docs/README.md#review-before-installing)。

## 上线前确认

- 每个本地数据目录只运行一个服务实例；不支持共享网络文件系统和 public URL 子路径。
- 安装器下载仅走本服务。Codex 运行期/API 流量不会被改写，抑制安装器更新标记也不会禁用所有 CLI 更新检查；请实施企业出口策略。
- Windows/macOS 安装和生产上游下载链仍需上线验证。

高级配置、备份、设计、验收及开发说明见[文档导航](docs/README.md)。详细参考文档目前使用中文。

## 许可

RedApp 原创代码采用 [MIT 许可](LICENSE)。[第三方许可](third_party/README.md)（包括 Codex 安装器 LICENSE/NOTICE）独立保留。
