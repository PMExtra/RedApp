# RedApp 运维说明

## 配置

命令行参数优先于对应环境变量。数据目录优先级固定为 `--data` > 非空 `REDAPP_DATA` > `/var/lib/redapp`，程序、Docker 和宿主系统服务统一使用该默认目录。不会探测运行环境、在权限失败后回退或自动迁移旧 `/data`；目录不可写时启动失败。宿主部署应由管理员提前创建目录并赋予服务用户所有权，本地开发显式使用 `--data ./data`。

| 参数 | 环境变量 | 默认值 | 含义 |
| --- | --- | --- | --- |
| `--data` | `REDAPP_DATA` | `/var/lib/redapp` | SQLite 与缓存所在本地目录 |
| `--listen` | `REDAPP_LISTEN` | `:8080` | HTTP 监听地址 |
| `--public-url` | `REDAPP_PUBLIC_URL` | 空 | 可选企业对外 HTTP(S) origin；空时逐请求推导 |
| `--base-url` | `REDAPP_BASE_URL` | `https://releases.openai.com/codex` | 服务端固定上游 HTTPS 根，客户端不能覆盖 |
| `--trusted-proxies` | `REDAPP_TRUSTED_PROXIES` | 空 | 可信代理 CIDR，逗号分隔 |

latest 默认 TTL 为 60 秒，管理员通过设置 API 可调整为 1–86400 秒；配置保存至 SQLite，按原成功 fetched 时间重新计算有效期。release 和完成制品不自动过期。过期 latest 刷新失败返回 502，不把旧值伪装为新值。

当前保守边界：最多 16 个活动写入、512 个制品读者、32 个 metadata flight、每资源最大 4 GiB、清单最大 4 MiB/1024 资产、JSON 深度 32。上游总请求超时 5 分钟，响应头 30 秒、连接/TLS 10 秒；每代最多 3 次顺序尝试。不安全续传至多自动创建一个完整重试的新代；原响应失败，不能把新文件拼接到旧前缀。服务器请求头 10 秒、请求读取 30 秒、写响应 10 分钟，正常停止最多等待 15 秒。SQLite busy timeout 为 5 秒。失败事件最多 1000 条/30 天，管理接口展示最近 100 条。

首次随机密码仅输出一次，bcrypt cost=12；会话有效期 8 小时，最多 128 个，修改密码注销全部会话。每来源 IP 每 5 分钟最多 10 次登录尝试，限流表最多 4096 个来源。会话 Cookie 使用 HttpOnly/SameSite=Strict，本次请求的对外 origin 为 HTTPS 时 Secure。变更 API 要求会话和 `X-CSRF-Token`，所有管理请求拒绝跨 origin 来源。

## 反向代理

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host codex.example.internal;
    proxy_set_header Forwarded "for=$remote_addr;proto=https;host=codex.example.internal";
    proxy_buffering off;
    proxy_read_timeout 600s;
}
```

只在直连 peer 命中可信 CIDR 时解析转发头。合法 Forwarded 优先于 X-Forwarded-For；从右往左剥离可信节点，停在首个不可信 IP。畸形 Forwarded 不转而拼接 XFF 链，回退至 peer。支持引号、IPv6 和多跳。显式 public URL 优先，且请求 Host 必须与配置的 origin host 完全一致。未配置时，使用合法请求 Host 与 TLS/HTTP scheme；仅可信直连 peer 可以通过 Forwarded 或 X-Forwarded-Host/Proto 提供对外 origin。Forwarded 从右向左选择信任边界对应节点；多值 X-Forwarded-Host/Proto 必须与 XFF 长度一致，单值要求直连可信代理覆盖设置。畸形的可信 origin 头返回 400，不混用两套头。反代必须删除/覆盖客户端传入的转发头，不要信任覆盖公网客户端的 CIDR。

安装器、metadata、管理状态和 Cookie 按本次请求使用同一 origin，不修改全局配置或上游 metadata 缓存；安装器与 metadata 响应禁用缓存。合法 Host 仅说明格式可安全使用，并不证明域名可信；自动模式允许不同合法 Host，严格域名边界请显式设置 public URL 并在反代限制 Host。管理请求仍进行同源和 CSRF 检查。管理页默认提供 `curl ... | sh` 与 `irm ... | iex` 命令；复制按钮需要 HTTPS 或 localhost 的浏览器剪贴板权限，否则手动复制。

## 回源代理

管理员可通过 `GET /admin/api/proxy` 和带会话/CSRF 的 `POST /admin/api/proxy` 配置统一回源代理。`server` 使用带明确端口的 `http://host:port`、`https://host:port` 或 `socks5://host:port`，禁止在 URL 中包含用户名、密码、路径或查询；空值直连，不继承 HTTP_PROXY/HTTPS_PROXY 等环境变量。HTTP 代理通过 CONNECT 访问 HTTPS 上游。SOCKS5 由代理端解析目标域名；本服务无法校验代理最终解析出的 IP，代理本身须可信。仍保留固定上游 URL/路径、逐跳重定向限制和端到端 TLS 证书校验，不提供任意 URL 代理。

请求包含 `password_action`：`keep` 保留已存用户名和密码（不得提交 username/password）；`replace` 使用此次 username/password；`clear` 清除全部凭据。更换代理地址时不能将已有凭据静默保留，须显式替换/清除；空 server 清除凭据。查询只返回 server、has_credentials、has_password、dns，不回显用户名/密码。凭据保存在本地 SQLite，并由数据目录权限保护，未做额外存储加密；数据库备份应作为敏感材料保护。

配置先成功持久化，再原子切换 transport。新 HTTP 请求（含 metadata、制品、重试/续传和重定向下一跳）采用新配置；已在传输的响应不被取消，旧空闲连接关闭。此设置不强制刷新已缓存 metadata/制品，也不会改写既有代际完整性校验。上游连接错误对客户端和事件使用通用消息，不输出代理 URL 凭据。

## 数据与恢复

- `instance.lock` 是永久保留的内核锁文件。进程始终持有 fd，退出或崩溃由内核释放；**禁止删除锁文件**。目录先解析符号链接，第二实例非阻塞失败。
- `state.sqlite` / WAL / SHM 存放 schema=1、授权清单原文与解析索引、首次发现历史、代际状态、当前指针、管理员 hash、设置、统计、事件和清理快照。
- `objects/<SHA256 身份>/<随机代际>.part|.blob` 由程序产生；URL 不直接映射本地路径。
- 每代共享一个顺序写入和增长文件。读者从零维护独立 offset；慢读者/客户端退出不会取消任务。无读者的完成缓存不长期占用文件描述符。
- 完成后计算完整 SHA-256，再 fsync、rename、目录 fsync 和提交 SQLite。启动重新核对文件与 hash，处理 rename 尚未提交、缺文件、坏 blob、遗留 part 和 tombstone/orphan；缺失或未验证文件不能作为 complete 返回。
- 续传采用落盘实际大小，强 ETag 使用 If-Range，严格验证 206 起点/终点/总量/validator/编码。416 仅在文件完整且 hash 已通过时接受；弱 ETag 不用于 If-Range，最终 hash 仍必需。
- 清理预览保存精确资源/代际集合，有效 10 分钟。执行先持久化 retiring，再摘除当前指针；新请求用新代，旧读者/写入者排空后删除。旧写入者不能发布成当前缓存。first_seen、清单和历史保留。
- 数据盘不支持 NFS/SMB 共享挂载。只读根文件系统配合可写本地持久卷运行。
- Docker 构建阶段预建 `/var/lib/redapp`，以 UID/GID `65532:65532`、模式 `0700` 复制到运行镜像；新建空命名卷由 Docker 初始化并继承目录权限，无需首次启动手工授权。任意已有宿主 bind mount 会覆盖镜像目录，其权限必须由管理员预先设置，非 root 镜像不会自动修正或递归 chown。v0.2.0 镜像使用新路径；旧 v0.1.0 镜像仍预建 `/data`，不能搭配新的卷路径。

备份时先正常停止服务，再复制整个数据目录（包括可能存在的 WAL/SHM）；恢复时确认无服务持锁，完整恢复后启动。不要在线只复制 `state.sqlite`。本版本不提供在线备份端点。跨 schema 降级启动会拒绝未知版本。

## 管理 API 与指标

公共入口：`GET /channels/latest`、`GET /releases/{version}/release.json`、精确清单授权的 `/releases/{version}/{asset}`、`/install.sh`、`/install.ps1`、`/licenses/{LICENSE|NOTICE}`。任意 URL、编码别名、查询参数、遍历与未授权资产拒绝。浏览器安装页面为 `/admin/`。

管理 API：`POST /admin/api/login` → Cookie 和 csrf；`GET /admin/api/session`、`GET /admin/api/status`；带 csrf 的 `POST /admin/api/settings`（latest_ttl_seconds）、`password`（old/new）、`logout`、`cleanup/preview`（minimum_version）、`cleanup/execute`（cleanup_id）。请求体为 JSON，最大 8 KiB。下载无需管理员登录；企业网络访问控制在反代/网络层实施。

`/health/live` 不依赖上游，`/health/ready` 检查 SQLite 和目录可写，不要求外网在线；健康请求不计入业务访问。`redapp healthcheck` 使用环境配置的监听端口和 public Host。

持久统计包括上下游实际字节、访问次数、命中/共享跟随/未命中、成功/失败、版本访问和清理释放字节。进程最近五秒速率另附采样时间。成功资源平均有效速度为最终大小/回源开始至收齐字节耗时，包含重试/退避，排除完整文件验证耗时。数据库计数与文件系统并非每字节原子事务，崩溃边界可能有少量统计偏差；完整性与发布不依赖统计。

磁盘 used 按数据树文件与目录的已分配块计算，包含 SQLite/WAL/SHM；另提供 logical 总字节和缓存/临时/待释放的逻辑量与已分配块分类，free 是文件系统 `bavail`。释放累计统计为逻辑字节，不能当作包含块舍入、稀疏文件和文件系统保留块的物理释放量。并发快照可能有短暂差异，待旧租约结束后归零。

## 指标历史

管理页点击任意全局指标打开近 7 天历史，可切换 24 小时和 30 天。默认每分钟采样、每小时聚合；24h 返回分钟原始点，7d/30d 返回小时汇总，分别保留 24h/30d。缺失点留空，当前未完小时标记 partial，计数器不平均累计量、不把重启/归零/断档当作零增量。完整目录、覆盖语义与 API 见[指标历史](metrics-history.md)。版本与单资源不保存历史维度。

## 上游和发布门禁

生产客户端仅允许固定上游 origin，禁止私有/loopback/link-local/multicast DNS 地址，逐跳验证重定向、路径、协议、无查询凭据；不猜测额外 CDN 域名，也不启用服务器 GitHub 回退。真实 CDN allowlist、原始清单/manifest fixture、历史 legacy/预发行真实样本仍待核验。如果官方资产跳出固定 origin，本服务失败关闭，维护者核验实际链后再扩充严格允许范围。

安装器原文、固定 commit、摘要、patch、generated、LICENSE/NOTICE 分离保存。CLI updater 在同文件系统临时树获取、校验、严格 patch、离线测试，再通过 Linux renameat2 交换整个目录；没有通过检查就不会改动目标。离线流程已测试；新版联网获取流程仍待验证。新版本仍需人工审阅 diff。

企业安装器抑制自动更新标记，保留官方正常交互语义。推荐无人值守环境显式 `CODEX_NON_INTERACTIVE=1`；交互选择启动 CLI 或包管理器子进程仍受企业出口策略控制。固定源码的 TUI 更新检查访问 GitHub，standalone 更新动作和 daemon 更新器获取公共安装器；删除 marker 不等于改写这些二进制路径。来源：[updates.rs](https://github.com/openai/codex/blob/ff6aec96948b70d94983af2641a6b67c94faeff5/codex-rs/tui/src/updates.rs)、[update_action.rs](https://github.com/openai/codex/blob/ff6aec96948b70d94983af2641a6b67c94faeff5/codex-rs/tui/src/update_action.rs)、[update_loop.rs](https://github.com/openai/codex/blob/ff6aec96948b70d94983af2641a6b67c94faeff5/codex-rs/app-server-daemon/src/update_loop.rs)。

正式外部分发之前，仍需核查六平台真实 package 的 LICENSE/NOTICE 和捆绑第三方材料，不改写官方 archive 字节或摘要。Windows/PowerShell 以及真实 macOS 的运行验证未完成。
