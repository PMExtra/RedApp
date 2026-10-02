# RedApp 运维说明

本文适用于采用规范 `vendor/app` 身份的新架构。旧版配置、缓存、历史全部不导入；必须选择全新空目录，旧目录保留归档，不自动升级或删除。新格式目录仍可正常重启。带版本号的历史文档不替代本说明。

## 启动配置

```sh
redapp config validate --config /etc/redapp/config.json
redapp serve --config /etc/redapp/config.json
redapp healthcheck --config /etc/redapp/config.json
```

三个命令都要求显式配置文件；validate 只验证配置，不打开或初始化部署数据库。配置示例为 [config/example.json](../config/example.json)。拒绝未知字段、重复 JSON key、null、错误类型和越界值，不猜测旧配置版本。

| JSON 字段 | 必填/默认 | 含义 |
| --- | --- | --- |
| `schema_version` | 必填，1 | 部署文件格式版本，与 SQLite schema 分开 |
| `data_dir` | 必填，绝对路径 | 新架构 SQLite 和制品目录 |
| `allowed_hosts` | 必填，1–128 项 | 认可的有效入站 authority 精确集合，含非默认端口，不允许 wildcard |
| `listen` | `:8080` | IP 或 localhost 加 1–65535 端口 |
| `trusted_proxies` | 空列表，最多 128 项 | 可提供转发信息的直接/链式代理 CIDR |
| `download_limits.max_active_writers` | 16，范围 1–1024 | 所有应用共享的活动写入上限 |
| `download_limits.max_readers` | 512，范围 1–65536 | 所有应用共享的制品读者上限 |
| `download_limits.max_artifact_bytes` | 4294967296，范围 1–1099511627776 | 单个制品的最大字节数 |

不再读取旧的逐字段启动参数、数据目录/监听/上游覆盖环境变量。上游地址、应用协议和信任根来自经过审查的编译期定义，不能在后台改成任意 URL。唯一保留的应用部署环境变量是新需求明确提供的 `REDAPP_PUBLIC_URL`。目录权限失败直接退出，不寻找备用目录。

站点文案、回源代理和公共地址覆盖保存在全局 settings；渠道 TTL 以规范 app_id 独立保存。TTL 默认 60 秒，范围 1–86400。GET 返回默认设置时 revision=0，不自动创建记录；PUT 要求 `If-Match: "<revision>"`，冲突返回 409，成功保存递增 revision。

## 公共地址与入站信任

生成安装命令和分发链接的公共地址优先级固定为：**后台覆盖 > `REDAPP_PUBLIC_URL` > 安全请求 origin**。环境空值表示未配置，非法非空值阻止启动。后台提交 `{"override_url":null}` 撤销覆盖，重新使用环境值或请求 origin；不是绕过环境值的“强制自动”模式。设置响应包含编辑值、环境值、有效值、来源和 revision。

公共地址仅接受 HTTP(S) origin，允许规范化一个尾 `/`，拒绝凭据、子路径、query、fragment 和注入字符。保存时不探测网络；持久化成功后新请求立即采用该地址，失败不改变运行状态。公共 bootstrap 和动态 installer 使用 no-store；已经复制出去的旧命令不会自动改写。

**请求 origin、公共地址、上游 origin 独立。** 请求 origin 按入站 Host、可信代理和 `allowed_hosts` 校验，决定同源校验与 Cookie Secure。改变公共地址不扩大 Host allowlist、不改变上游授权，也不跳转当前后台。

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host downloads.example.internal;
    proxy_set_header Forwarded "for=$remote_addr;proto=https;host=downloads.example.internal";
    proxy_buffering off;
    proxy_read_timeout 600s;
}
```

将实际代理网段写入 `trusted_proxies`，并把 `downloads.example.internal` 写入 `allowed_hosts`。仅可信 peer 的转发头生效；合法 Forwarded 优先于 X-Forwarded-*，沿链从右向左选择信任边界。多值 Host/Proto 必须与 XFF 长度一致，单值须由直连可信代理覆盖。畸形可信 origin 返回 400，不混用两套头。代理必须删除或覆盖客户端转发头，不应信任覆盖公网客户端的 CIDR。

首次随机管理员密码只输出一次；登录后修改并保护日志。会话 Cookie 为 HttpOnly、SameSite=Strict、Path=/admin，按请求安全 origin 决定 Secure；管理写入要求会话、同源校验和 `X-CSRF-Token`。修改密码使现有会话失效。

## 站点文案与回源代理

`/admin/settings/site` 同时编辑英文、简体中文标题、副标题和声明，并提供独立公共地址表单。默认品牌保留 RedApp；副标题为 Application Redistribution Platform / 应用再分发平台。标题每语言必填、最多 80 字符，副标题最多 160、声明最多 500；文本不执行 HTML/Markdown。

`GET/PUT /admin/api/settings/proxy` 管理共享出口代理，PUT 要求 revision 和会话/CSRF。server 为带端口的 `http://host:port`、`https://host:port` 或 `socks5://host:port`；URL 内禁止凭据、路径、查询、fragment，空 server 表示直连。不继承 HTTP_PROXY/HTTPS_PROXY。HTTP 代理以 CONNECT 访问 HTTPS 上游，SOCKS5 由代理端解析 DNS；固定上游路径、重定向和端到端 TLS 校验仍生效。

`password_action` 为 keep、replace 或 clear。keep 不接受 username/password，且不能将已有凭据静默带到新地址；replace 使用本次凭据，clear 清空凭据。GET 只返回 server、has_credentials、has_password、dns、revision。凭据存放在受目录权限保护的 SQLite，备份须按敏感材料保管。

代理先 CAS 持久化，再切换所有应用共享的 transport；失败不生效。新请求使用新配置，已开始传输的响应自然结束。此操作不强制失效 metadata、缓存制品或正在进行的摘要校验。

## 数据目录、清理与恢复

- `instance.lock` 是保留的内核锁文件；进程退出或崩溃后内核释放锁，禁止人为删除锁 inode。获取写锁前先以只读方式检查已有目录，拒绝旧 schema 或未知内容，不创建锁来污染被拒绝的旧目录。
- SQLite schema=3，包含类型化全局/应用设置、授权 metadata、渠道、资源、generation、应用内 blob、累计值、指标样本/聚合、事件、清理快照和管理员记录。没有旧版迁移命令。
- 逻辑资源身份为 `(app_id,version,resource_key)`。每次下载拥有独立随机 generation；完整 blob 按 `(app_id,sha256)` 复用，首版不跨应用物理去重。未完成文件位于 `objects/parts/<generation>.part`，完整文件位于 `objects/blobs/<app摘要>/<内容摘要>.blob`，URL 不直接映射磁盘路径。
- 活动下载仅按精确逻辑资源合流。完整校验、fsync 和文件发布后才能标记 complete；重启核对磁盘和数据库，损坏/缺失文件不能作为已验证缓存返回。续传使用强 ETag/If-Range 并验证范围、编码、长度和最终摘要。
- 清理预览冻结本应用的精确 generation 集合，有效 10 分钟；执行不能跨应用，成功回执支持重试。旧代退出当前状态后等待已有读写租约排空；应用内共享 blob 只在最后引用结束后回收。版本发现、可信 metadata 和累计指标不随缓存清理删除。
- 一个本地目录只由一个实例使用，不支持 NFS/SMB。Docker 可采用只读根文件系统加可写持久卷；镜像内 `/var/lib/redapp` 为 UID/GID 65532、模式 0700。已有宿主 bind mount 的权限需管理员预先设置，不递归自动 chown。

备份先正常停止服务，再复制整个新格式数据目录，包括可能存在的 WAL/SHM。恢复到同格式目录前确认没有服务持锁，不在线单独复制 state.sqlite。启动不会把旧格式目录转为新格式，也不会将旧历史导入。

## 路由、管理 API 与指标

公开目录为 `/`；应用详情为 `/<vendor>/<app>`，制品及 installer 位于 `/<vendor>/<app>/<file_path>`，当前是 `/openai/codex` 和 `/anthropic/claude-code`。`admin`、`api`、`assets`、`health` 为保留命名空间。旧 `/apps/codex`、根 `/install.sh` 和 `/api/info` 不提供兼容别名。`GET /api/bootstrap` 返回公开站点、应用定义和公共地址，不依赖管理 status。

后台页面有真实路径：`/admin/overview`、`/admin/events`、`/admin/settings/site`、`/admin/settings/proxy`、`/admin/apps/<vendor>/<app>/versions`、`.../settings`，可以刷新和直接打开。设置页不订阅全局 status 轮询。

| API | 用途 |
| --- | --- |
| `POST /admin/api/login`、`/logout`、`/password` | 登录、退出、改密码 |
| `GET /admin/api/session`、`/status`、`/events` | 会话、全局状态、事件 |
| `GET/PUT /admin/api/settings/site`、`.../proxy`、`.../public-url` | 带 revision 的全局设置 |
| `GET/PUT /admin/api/apps/<vendor>/<app>/settings` | channel_ttl_seconds 与 revision |
| `GET /admin/api/apps/<vendor>/<app>/status`、`.../versions`、`.../resources` | 明确应用状态 |
| `POST /admin/api/apps/<vendor>/<app>/cleanup/preview` | minimum_version 清理预览 |
| `POST /admin/api/apps/<vendor>/<app>/cleanup/<id>/execute` | 执行冻结预览 |
| `GET /admin/api/history?scope=global&metric=...&range=24h` | 全局指标历史 |
| `GET /admin/api/apps/<vendor>/<app>/history?metric=...&range=24h` | 有限应用维度历史 |

不存在默认 Codex 应用，也不接受应用选择 header 作为身份替代。资源 path、query、应用和授权均须通过服务端校验，未知 API 不回退成成功 HTML。

status 仅返回摘要和指标，列表从 versions、resources、events 单独读取。列表响应为 `{"items":[],"next_cursor":null}`；`limit` 默认 50、最大 100，下一页提交返回的非空 `cursor`。resources 可用 `version` 精确筛选。游标绑定应用、端点和筛选条件，不能在切换应用或版本后复用；版本按文本升序、资源按 generation ID 升序、事件按 ID 降序。分页是实时视图，不承诺跨请求冻结快照。

`/health/live` 不依赖上游；`/health/ready` 检查 SQLite 和目录可写，不要求外网在线。CLI healthcheck 读取部署文件，连接实际监听地址并使用允许的 Host，不受公共发布地址改变影响。健康请求不计入业务访问。

当前 active 采集目录为 41 项。已确认的 16 个常用/25 个诊断前端分组及图表 tooltip 由独立补丁交付，本分支尚待取得可校验补丁后整合。`reuse_requests` 不再独立写入/采样/展示；`events.recent_total` 不再采样/展示，事件详情保留。全局 versions.total 计算全部规范应用的版本记录。旧样本（若新格式库已有）自然过期，不通过未知 ID 删除历史；本次架构切换本身不导入旧库历史。

默认每分钟采样，每小时聚合；24h 使用分钟点，7d/30d 使用小时汇总，保留期分别 24h/30d。缺失点留空、当前未完整小时标记 partial；计数器不平均累计量，也不把重启或断档视为零增量。磁盘 used 为已分配块，free 为文件系统可用空间，清理释放累计为逻辑字节。计数和磁盘状态不是每字节原子事务，完整性校验与发布不依赖统计。单个版本/资源不扩展成无限历史标签。

## 上游与安装器边界

协议适配器校验版本、渠道、metadata、签名和授权制品；共享分发层仅允许对应编译期上游 origin 和路径，限制重定向并禁止直接连接私有/loopback/link-local DNS 地址。新增 CDN 或信任根须审查代码，失败时不自动猜测来源。

安装器原文、patch、generated、provenance 和许可分离保存；统一 descriptor 驱动产物清单和固定验证器。更新须通过严格 patch、隔离离线检查及宿主重验，daily PR 不可顺带改 descriptor、信任根或验证代码。详见[安装器维护](installers-maintenance.md)。

安装器下载经本服务，不改写应用运行期/API 流量。抑制安装器更新标记不等于禁用全部 CLI 更新检查；Windows/PowerShell、macOS、官方真实制品和生产下载链仍需独立上线验证。
