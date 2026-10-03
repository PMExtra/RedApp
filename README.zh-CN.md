# RedApp

[English](README.md)

RedApp 帮助 IT 管理员通过内网服务分发 **Codex CLI 和 Claude Code**，验证应用元数据、按需缓存制品，并管理缓存、流量与站点设置。

v0.6.0 统一使用 `openai/codex`、`anthropic/claude-code` 应用身份，是**必须使用全新空数据目录的破坏性升级**。旧版配置、缓存和历史全部不导入；旧目录保留归档，不自动升级或删除。启动检测到旧版或未知目录会拒绝继续。新架构已创建的目录可以正常重启使用。升级前请阅读 [v0.6.0 发布说明](docs/multi-application-v0.6.0.md)。

## 快速上手

默认无需配置文件。原生程序执行 `redapp` 或 `redapp serve` 即可；镜像默认执行 `serve`，健康检查使用相同的配置解析规则。

```sh
docker run -d --name redapp --read-only \
  -p 127.0.0.1:8080:8080 \
  -v redapp-v06-data:/var/lib/redapp \
  ghcr.io/pmextra/redapp:0.6.2
```

默认监听 `:8080`、数据目录 `/var/lib/redapp`、不信任任何反向代理，Host 仅允许监听端口上的 `localhost`、`127.0.0.1`、`[::1]`。使用远程域名/IP 或不同映射端口时，明确设置 `REDAPP_ALLOWED_HOSTS`；PUBLIC_URL 不自动扩大 Host 列表。首次部署使用新的空目录/卷，已有 v0.6 数据可继续使用；权限失败不会寻找备用目录。

可以只用环境变量启动：

```sh
REDAPP_DATA=/absolute/writable/redapp-data \
REDAPP_LISTEN=127.0.0.1:8080 \
REDAPP_ALLOWED_HOSTS=localhost:8080,127.0.0.1:8080 \
  redapp
```

配置路径选择为 **`--config FILE` > `REDAPP_CONFIG` > `/etc/redapp/config.yaml`**，只读取选中的一个文件。默认 YAML 存在则读取，不存在也可启动；手动指定的文件缺失、选中文件非法/不可读都会报错。空 `REDAPP_CONFIG` 视为未指定。不自动探测 `config.json` 或工作目录文件。已有 JSON 部署可以设置 `REDAPP_CONFIG=/etc/redapp/config.json`，让服务和健康检查继续读取同一文件。

可选的 [YAML 示例](config/example.yaml) 展示默认值；不需要的字段可以省略。手动选择 `.json` 文件仍兼容 [JSON 示例](config/example.json)。部署字段优先级是 **命令行参数 > 环境变量 > 所选文件 > 内建默认值**；列表整体替换，不合并。各来源单独校验，覆盖值不会掩盖已选文件的错误。配置必须是单个映射，拒绝未知字段、重复键、null、类型/范围错误及 YAML 锚点、别名、合并键。验证命令不打开或创建数据目录。

```sh
redapp config validate
redapp config validate --config ./config.yaml
REDAPP_CONFIG=/etc/redapp/config.json redapp serve
redapp serve --config ./custom.yaml --listen 127.0.0.1:8081
```

容器可选挂载 YAML：在上述命令中增加 `--mount type=bind,src=/absolute/config.yaml,dst=/etc/redapp/config.yaml,readonly`。自定义 YAML/JSON 路径使用挂载加 `REDAPP_CONFIG`。若通过覆盖容器命令传入 `--config` 或其他部署参数，应在 `--health-cmd` 中传入等价参数；Docker 不会将 CMD 参数传给 HEALTHCHECK。全部字段、环境变量、CLI 和限额见[运维说明](docs/operations.md#启动配置)。上游地址和信任根继续来自编译期定义。

打开 **http://localhost:8080/** 浏览应用；管理员入口为 **http://localhost:8080/admin/overview**。初始管理员密码只写入首次启动日志，登录后请修改密码并保护日志。容器以 UID/GID 65532 运行，宿主 bind mount 的权限须提前设置。

待发布源码支持在环境变量、CLI 和 YAML/JSON 中使用 `REDAPP_MAX_ARTIFACT_BYTES=4GiB` 等容量简写，兼容原有整数字节值。这是**单个制品文件**的上限，不是缓存总体积：`4GB`/`4gb` 表示 4000000000 字节，`4GiB` 表示 4294967296 字节。默认保持 4 GiB，允许范围为 1 字节至 1 TiB。已发布 v0.6.2 仍需使用整数字节值，见[版本说明](docs/releases.md)。

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
