# RedApp

[English](README.md)

RedApp 帮助 IT 管理员通过内网服务分发 **Codex CLI 和 Claude Code**，验证应用元数据、按需缓存制品，并管理缓存、流量与站点设置。

v0.6.0 统一使用 `openai/codex`、`anthropic/claude-code` 应用身份，是**必须使用全新空数据目录的破坏性升级**。旧版配置、缓存和历史全部不导入；旧目录保留归档，不自动升级或删除。启动检测到旧版或未知目录会拒绝继续。新架构已创建的目录可以正常重启使用。升级前请阅读 [v0.6.0 发布说明](docs/multi-application-v0.6.0.md)。

## 快速上手

复制 [config/example.json](config/example.json) 为部署文件，按实际域名和端口修改 `allowed_hosts`。示例只允许 `localhost:8080`，不支持通配 Host；`data_dir` 必须为绝对路径。

```sh
redapp config validate --config /etc/redapp/config.json
redapp serve --config /etc/redapp/config.json
redapp healthcheck --config /etc/redapp/config.json
```

验证命令不打开或创建数据目录。必须提供配置文件及 `schema_version`、`data_dir`、非空 `allowed_hosts`；其余字段默认值和合法范围见[运维说明](docs/operations.md)。未知字段、重复 JSON key、null、错误类型及越界值均拒绝。

挂载配置文件并创建新的命名卷：

```sh
docker run -d --name redapp --read-only \
  -p 127.0.0.1:8080:8080 \
  -v redapp-v060-data:/var/lib/redapp \
  -v /etc/redapp/config.json:/etc/redapp/config.json:ro \
  ghcr.io/pmextra/redapp:0.6.0
```

镜像默认执行 `serve --config /etc/redapp/config.json`，健康检查读取同一配置。每个本地数据目录只运行一个实例，不支持网络共享文件系统或 URL 子路径。旧逐字段 CLI 和 `REDAPP_DATA`、`REDAPP_LISTEN`、上游覆盖环境变量不再使用；上游地址和信任根来自编译期应用定义。

打开 **http://localhost:8080/** 浏览应用；管理员入口为 **http://localhost:8080/admin/overview**。初始管理员密码只写入首次启动日志，登录后请修改密码并保护日志。容器以 UID/GID 65532 运行，宿主 bind mount 的权限须提前设置。

## 公共地址与代理

安装链接使用全局公共地址，优先级为：**后台持久化覆盖 > `REDAPP_PUBLIC_URL` 环境默认 > 经验证的请求 origin**。后台清空覆盖会恢复环境默认（若有）；界面显示有效值和来源。只接受无凭据、子路径、查询或 fragment 的 HTTP(S) origin。非空但非法的环境值会阻止启动。

公共地址不改变入站 Host 信任、Cookie 安全属性或上游下载地址。企业访问使用 HTTPS 反向代理，将可信代理 CIDR 写入 `trusted_proxies`，并让代理覆盖转发头；有效 Host 仍须在 `allowed_hosts` 中。来自不可信 peer 的转发头会被忽略。

站点文案、回源代理和公共地址属于全局设置；渠道 TTL 按应用独立保存。每次保存校验 revision，过期表单不会静默覆盖更新。回源代理凭据不通过读 API 返回。

## 客户端安装

将 `downloads.example.internal` 换为服务地址。应用详情位于 `/openai/codex` 和 `/anthropic/claude-code`：

```sh
curl -fsSL https://downloads.example.internal/openai/codex/install.sh | sh
curl -fsSL https://downloads.example.internal/anthropic/claude-code/install.sh | sh
```

```powershell
irm 'https://downloads.example.internal/openai/codex/install.ps1' | iex
irm 'https://downloads.example.internal/anthropic/claude-code/install.ps1' | iex
```

命令会下载并执行安装器，请按部署策略先审查脚本。Codex 使用已设置的 `CODEX_RELEASE`，否则选择 `latest`；无人值守 shell 安装时，将 `CODEX_NON_INTERACTIVE=1` 放在管道中 `sh` 前面。指定版本的命令由应用详情页提供。旧公共路径和隐式选择 Codex 的管理 API 不再提供别名。

安装器下载经过本服务，应用运行期/API 流量不改写。生产上游链路与 Windows/macOS 实机安装仍需上线验证；客户端出口策略独立管理。

## 开发与许可

在 `frontend/` 执行 `npm ci && npm run build` 后，Go 构建会嵌入前端产物。构建前运行 Go 测试和前端类型/DOM 检查；`scripts/test-data-cli.py`、`scripts/test-http-cli.py` 使用 `bin/redapp` 和隔离临时目录验证实际 CLI。

实现方案见[架构文档](docs/multi-application-architecture-next.md)。带旧版本号的文档描述对应历史版本，不是当前开发架构的配置指南。

RedApp 原创代码采用 [MIT 许可](LICENSE)。[第三方许可](third_party/README.md)，包括上游安装器 LICENSE/NOTICE，独立保留。
