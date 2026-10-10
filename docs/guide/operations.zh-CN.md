# 运维

[English](operations.md)

本文介绍在生产环境运行 RedApp：容器、数据、反向代理、健康检查、管理后台、备份和故障排查。设置项见[配置](configuration.zh-CN.md)。

## Docker

镜像基于 `scratch` 构建，只包含 `redapp` 程序和 CA 证书包。

| 属性 | 值 |
| --- | --- |
| 用户 | UID/GID `65532` |
| 端口 | `8080` |
| 数据卷 | `/var/lib/redapp`（权限 `0700`） |
| 命令 | `/redapp serve` |
| 健康检查 | `/redapp healthcheck`，每 30 秒一次，超时 5 秒，重试 3 次 |
| 可选配置文件 | `/etc/redapp/config.yaml` |

推荐的运行命令：

```sh
docker build -t redapp:local .
docker run -d --name redapp --read-only \
  -p 127.0.0.1:8080:8080 \
  -v redapp-data:/var/lib/redapp \
  -e REDAPP_PUBLIC_URL=https://downloads.example.internal \
  redapp:local
```

- RedApp 只写数据目录，因此可以使用 `--read-only`。
- 绑定挂载的数据目录必须可由 UID/GID `65532` 写入。RedApp 不会修改属主，也不会改用其他目录。
- 使用配置文件时，可以只读挂载到 `/etc/redapp/config.yaml`，或挂载到其他位置并把 `REDAPP_CONFIG` 设为其容器内路径。

### 健康检查参数

镜像的健康检查以无参数方式运行 `/redapp healthcheck`。Docker 不会把容器命令的参数传给它。

优先使用环境变量和配置文件：服务和健康检查都会读取它们。如果用 `--listen` 等参数覆盖了命令，健康检查也要使用相同参数。

镜像中没有 shell，因此健康检查必须使用 exec 形式。`docker run --health-cmd` 使用 shell 形式，不适用于本镜像。请改用 Docker Compose：

```yaml
services:
  redapp:
    image: redapp:local
    read_only: true
    command: ["serve", "--listen", ":9090"]
    healthcheck:
      test: ["CMD", "/redapp", "healthcheck", "--listen", ":9090"]
    volumes:
      - redapp-data:/var/lib/redapp
volumes:
  redapp-data:
```

`redapp healthcheck` 通过回环地址连接配置的监听端口并请求 `/health/ready`。它不使用公共地址，也不使用出口代理。

## 数据目录

数据目录保存全部运行状态：

| 路径 | 内容 |
| --- | --- |
| `state.sqlite`（及 `-wal`、`-shm`） | 配置、账号、缓存索引、指标、事件 |
| `objects/` | 缓存的发布文件、HTTP 缓存正文、托管文件、图标 |
| `instance.lock` | 防止第二个实例使用的锁 |

- 一个目录只供一个实例使用。不支持网络文件系统（NFS、SMB）。
- 不要删除 `instance.lock`。进程退出时操作系统会释放该锁。
- 目录必须是绝对路径。目录不存在时，RedApp 以 `0700` 权限创建。

1.0 之前，数据目录在不同数据库 schema 版本之间不兼容：新版本会拒绝旧版本写入的数据目录，且不提供迁移。使用新版本时请使用新的空数据目录，并在可能回退时保留旧目录。

被拒绝的目录不会被修改。

## 反向代理

除本地测试外，请在 HTTPS 反向代理之后运行 RedApp。

1. 在代理终止 TLS，并转发到 RedApp 的监听地址。
2. 在代理限制允许的域名。RedApp 接受任何语法合法的 `Host`。
3. 让代理覆盖客户端发送的转发头。
4. 把代理的地址范围加入 `trusted_proxies`。
5. 关闭响应缓冲并允许长时间传输，因为下载文件可能很大。

nginx 示例：

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host downloads.example.internal;
    proxy_set_header Forwarded "for=$remote_addr;proto=https;host=downloads.example.internal";
    proxy_set_header X-Forwarded-For "";
    proxy_set_header X-Forwarded-Proto "";
    proxy_set_header X-Forwarded-Host "";
    proxy_buffering off;
    proxy_request_buffering off;
    proxy_read_timeout 600s;
    client_max_body_size 0;
}
```

```sh
REDAPP_TRUSTED_PROXIES=127.0.0.1/32 redapp serve
```

RedApp 读取转发头的方式：

- 只有直连对端位于 `trusted_proxies` 时才使用转发头。
- 合法的 `Forwarded` 头优先于 `X-Forwarded-*`。
- 转发链从右向左读取，直到第一个不可信地址。
- 可信代理发送的畸形转发头会得到 `400 Bad Request`。

请设置 `REDAPP_PUBLIC_URL`（或后台中的覆盖值），使安装命令使用你的公共 HTTPS 地址。

## 健康检查端点

| 端点 | 检查内容 | 成功 |
| --- | --- | --- |
| `GET /health/live` | 进程能响应 HTTP | `200` |
| `GET /health/ready` | 数据库可响应且数据目录可写 | `200`；否则 `503` |

两个端点都不访问上游，因此外网中断不会使 RedApp 变为不健康。健康检查请求不计入指标。

## 管理后台

在 `/admin/login` 登录。可以直接打开或收藏的页面：

| 路径 | 页面 |
| --- | --- |
| `/admin/overview` | 概览：服务状态和带历史图表的指标 |
| `/admin/events` | 事件：近期警告和失败 |
| `/admin/settings/site` | 站点外观：标题、副标题、声明、公共地址、首页置顶 |
| `/admin/settings/proxy` | 回源代理：全局出口代理 |
| `/admin/vendors` | 厂商与应用：列表、搜索、导入 |
| `/admin/vendors/new` | 创建厂商 |
| `/admin/categories` | 重命名分类 |
| `/admin/vendors/<vendor>/settings` | 厂商设置、导出 |
| `/admin/vendors/<vendor>/apps` | 厂商下的应用 |
| `/admin/vendors/<vendor>/apps/new` | 创建应用 |
| `/admin/vendors/<vendor>/admin-notes` | 厂商的私有备注 |
| `/admin/vendors/<vendor>/apps/<app>/settings` | 应用设置、使用说明、导出、复制 |
| `/admin/vendors/<vendor>/apps/<app>/admin-notes` | 应用的私有备注 |
| `/admin/vendors/<vendor>/apps/<app>/versions` | 版本与资源（Codex、Claude Code） |
| `/admin/vendors/<vendor>/apps/<app>/cache` | 缓存管理（HTTP 缓存、Codex、Claude Code） |
| `/admin/vendors/<vendor>/apps/<app>/files` | 托管文件（仅 Hosted Provider） |

公开页面：

| 路径 | 页面 |
| --- | --- |
| `/` | 首页：置顶应用和近 7 天下载排行 |
| `/all` | 全部已发布应用的目录，支持搜索、分类和分页 |
| `/<vendor>` | 厂商页面及其已发布应用 |
| `/<vendor>/<app>` | 应用页面及使用说明 |
| `/<vendor>/<app>/<path>` | 文件、安装器和发布下载 |

厂商 ID `all`、`admin`、`api`、`assets` 和 `health` 为保留名称。

## 后台维护

以下任务在服务内部运行，每 15 分钟一次。启动时都不运行；首次运行在服务启动 15 分钟后。

| 任务 | 作用 |
| --- | --- |
| HTTP 缓存清理 | 执行 HTTP 缓存应用的自动清理规则 |
| 发布版本保留 | 启用时，为 Codex 和 Claude Code 应用保留最新 N 个已缓存版本 |
| 自动预热 | 启用时，为配置的渠道和平台下载新发布 |
| 预览过期 | 移除过期的清理预览 |

指标采样在启动时运行一次，之后每分钟一次。

## 日志与事件

RedApp 只向标准错误输出少量日志：

- 首次启动时打印一次初始管理员密码
- `RedApp started: listener ..., data directory ...`
- 指标采样和自动缓存清理的失败
- 启动失败时的致命错误

运行警告记录在 **事件** 页面而不是日志中，例如旧缓存回退、覆盖上游 `no-store` 的缓存规则、上游错误、下载失败和自动清理失败。RedApp 保留最新 1,000 条事件，最长 30 天。

## 备份

备份整个数据目录。它包含配置、账号、缓存文件、托管文件和历史。

1. 停止服务，使数据库处于一致状态。
2. 复制整个数据目录，包括可能存在的 `state.sqlite-wal` 和 `state.sqlite-shm`。
3. 重新启动服务。

服务运行时不要单独复制 `state.sqlite`。备份属于敏感数据：其中包含管理员密码哈希和出口代理凭据。

恢复时，停止服务并用备份替换数据目录。备份只能用于数据库 schema 相同的版本。

[配置导出](configuration.zh-CN.md#导出)是只针对设置的轻量替代方案。它不包含缓存文件、托管文件、历史或账号。

## 故障排查

| 现象 | 可能原因与处理 |
| --- | --- |
| 启动失败并提示 “belongs to another RedApp schema version” | 数据目录来自其他 schema 版本。使用新的空目录。 |
| 启动失败并提示 “already owned by another instance” | 另一个进程正在使用该目录。停止该进程，不要删除 `instance.lock`。 |
| 启动时出现权限错误 | 服务用户（Docker 中为 `65532`）无法写入目录。修正属主。 |
| 修改参数后容器不健康 | 健康检查看不到你的参数。使用环境变量，或在 exec 形式的健康检查中传入相同参数。 |
| 安装命令显示 `http://` 或内部主机名 | 设置 `REDAPP_PUBLIC_URL` 或公共地址覆盖值，并检查 `trusted_proxies`。 |
| 在代理后无法登录后台 | 转发的 scheme 或 host 与浏览器地址不一致。检查代理头和 `trusted_proxies`。 |
| 下载返回 `503` | 下载容量已满。调高 `max_writers` 或 `max_readers`，或稍后重试。 |
| 下载返回 `502` | 上游不可达或返回了不可信的元数据。检查出口代理和事件页面。 |
| 应用返回 `404` | 厂商或应用被禁用。两者都要启用。 |
| 重启后所有人都已退出登录 | 正常现象。管理员会话只保存在内存中。 |
| 忘记管理员密码 | 恢复备份，或使用新的空数据目录启动。没有重置命令。 |
