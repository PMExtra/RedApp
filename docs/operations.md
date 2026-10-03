# RedApp 运维说明

本文对应 v0.6.3 的配置规范；旧版本请使用[相应标签下的文档](https://github.com/PMExtra/RedApp/blob/v0.6.2/docs/operations.md)及样例。本文适用于采用规范 `vendor/app` 身份的新架构。旧版配置、缓存、历史全部不导入；必须选择全新空目录，旧目录保留归档，不自动升级或删除。新格式目录仍可正常重启。带版本号的历史文档不替代本说明。

## 启动配置

自 v0.6.2 起默认无需文件：`redapp` 和 `redapp serve` 等价；`redapp healthcheck` 使用相同解析规则。镜像 CMD 为 `serve`，HEALTHCHECK 为 `healthcheck`，都不硬编码 `--config`。

文件路径按 **`--config FILE` > `REDAPP_CONFIG` > `/etc/redapp/config.yaml`** 选择，只读取一个文件。空配置路径环境变量视为未指定。默认文件不存在允许继续；默认文件存在但非法/不可读，或手动指定文件缺失/非法，都失败退出。不读取默认 `config.json`，不搜索工作目录，不叠加默认文件。`.json` 按 JSON 解析，其他路径按 YAML 解析（也可承载 JSON 子集）。推荐 [config/example.yaml](../config/example.yaml)，[JSON 示例](../config/example.json) 也可显式选择。

部署字段逐层覆盖：**CLI > env > 所选文件 > 默认值**。未提供字段保留低层值；列表整体替换。每个来源都必须合法，低层错误不能由高层值掩盖。`schema_version` 可省略，提供时只能为 1。配置最大 64 KiB，必须是一个对象/映射；拒绝未知字段、重复键、null、类型/范围错误、多 YAML 文档、锚点/别名/合并键及自定义标签。除下述制品容量字符串外，文件中的整数不接受字符串或浮点形式。

| 字段 | 环境变量 | CLI | 默认 |
| --- | --- | --- | --- |
| `schema_version` | — | — | 1（部署格式，与 SQLite schema 分开） |
| `data_dir` | `REDAPP_DATA` | `--data` | `/var/lib/redapp`，必须为绝对路径 |
| `listen` | `REDAPP_LISTEN` | `--listen` | `:8080` |
| `trusted_proxies` | `REDAPP_TRUSTED_PROXIES` | `--trusted-proxies` | 空列表 |
| `download_limits.max_writers` | `REDAPP_MAX_WRITERS` | `--max-writers` | 16，范围 1–1024 |
| `download_limits.max_readers` | `REDAPP_MAX_READERS` | `--max-readers` | 512，范围 1–65536 |
| `download_limits.max_artifact_bytes` | `REDAPP_MAX_ARTIFACT_BYTES` | `--max-artifact-bytes` | 4 GiB（4294967296 字节），范围 1 字节–1 TiB |

`max_artifact_bytes` 限制每个下载制品文件的大小，不是应用总体积或缓存总配额；压缩包按下载文件大小计算。默认保持 4 GiB。自 v0.6.3 起支持环境变量、CLI、YAML 和显式 JSON 使用相同容量字符串，例如 `REDAPP_MAX_ARTIFACT_BYTES=4GiB`、`--max-artifact-bytes 1.5GiB`、YAML `max_artifact_bytes: '4GiB'` 或 JSON `"max_artifact_bytes": "4GiB"`。现有整数字节值仍有效，已发布 v0.6.2 应继续使用整数。

容量字符串去除首尾空白后，按 [go-humanize ParseBytes](https://pkg.go.dev/github.com/dustin/go-humanize@v1.1.0#ParseBytes) 解析，支持小数及大小写不敏感的单位。**GB = 10^9 字节，GiB = 2^30 字节**，所以 `4gb` 是 4000000000 字节，`4GiB` 是 4294967296 字节。推荐使用 `4GiB` 等明确单位，不使用逗号；纯数字表示字节。转换结果必须在 1–1099511627776 字节之间，非法或超范围值阻止启动。reader/writer 数量仍使用整数，不接受容量单位。

`trusted_proxies` 的环境变量与 CLI 使用逗号分隔列表，去除项两侧空白，最多 128 个有效 CIDR；空环境值/CLI 可清空列表。RedApp 接受语法合法的 Host，域名与网络访问策略由反向代理负责。PUBLIC_URL 只控制生成链接，不作为入站白名单。

`max_writers` 和 `max_readers` 都是全应用共享的并发上限。writer 槽位覆盖回源、重试等待和校验，同一制品的多个 reader 共用一个 writer；reader 包含等待下载或客户端读取的请求，缓存命中仍占 reader。超限请求返回 503，不进入容量等待队列。

从旧版升级配置时，删除 `allowed_hosts`、`REDAPP_ALLOWED_HOSTS`、`--allowed-hosts`，并将 writer 改用上表名称。旧 `max_active_writers` 文件字段、`--max-active-writers` 选项和已删除的 Host 文件字段/选项会报错；旧环境变量不再读取，不提供兼容别名。

```sh
# 无文件验证和启动；数据目录须可写
REDAPP_DATA=/absolute/writable/redapp-data redapp config validate
REDAPP_DATA=/absolute/writable/redapp-data redapp
# 可选文件，以及命令行覆盖环境变量路径
REDAPP_CONFIG=/etc/redapp/custom.yaml redapp serve
REDAPP_CONFIG=/ignored.yaml redapp config validate --config ./config.json
```

`validate` 不打开或初始化数据目录。容器默认无需挂配置；可选挂载 `/etc/redapp/config.yaml`，或挂载自定义文件后设置 `REDAPP_CONFIG`，使服务及健康检查使用一致来源。单独修改容器 CMD 的 CLI 参数时也须调整 HEALTHCHECK；推荐环境变量方式。使用 JSON 的部署应先按当前字段更新文件，再显式选择其路径，不能依赖自动发现。旧数据目录仍在初始化写入之前被拒绝，权限失败直接退出，不寻找备用目录。

上游地址、应用协议和信任根仍来自受审查的编译期定义，不恢复任意上游 URL 覆盖。`REDAPP_PUBLIC_URL` 不属于上述字段覆盖链，只给后台公共 URL 设置提供环境默认值；文件/CLI 不增加新的 public_origin 来源。

站点文案、回源代理和公共地址覆盖保存在全局 settings；渠道 TTL 以规范 app_id 独立保存。TTL 默认 60 秒，范围 1–86400。GET 返回默认设置时 revision=0，不自动创建记录；PUT 要求 `If-Match: "<revision>"`，冲突返回 409，成功保存递增 revision。

## 公共地址与入站信任

生成安装命令和分发链接的公共地址优先级固定为：**后台覆盖 > `REDAPP_PUBLIC_URL` > 安全请求 origin**。环境空值表示未配置，非法非空值阻止启动。后台提交 `{"override_url":null}` 撤销覆盖，重新使用环境值或请求 origin；不是绕过环境值的“强制自动”模式。设置响应包含编辑值、环境值、有效值、来源和 revision。

公共地址仅接受 HTTP(S) origin，允许规范化一个尾 `/`，拒绝凭据、子路径、query、fragment 和注入字符。保存时不探测网络；持久化成功后新请求立即采用该地址，失败不改变运行状态。公共 bootstrap 和动态 installer 使用 no-store；已经复制出去的旧命令不会自动改写。

**请求 origin、公共地址、上游 origin 独立。** 请求 origin 按入站 Host 语法和可信代理链校验，决定同源校验与 Cookie Secure。改变公共地址不限制入站 Host、不改变上游授权，也不跳转当前后台。管理 API 仍检查会话、Origin 和写操作的 CSRF；会话 Cookie 保持 host-only、`/admin`、HttpOnly、SameSite Strict，并按有效请求 origin 设置 Secure。

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host downloads.example.internal;
    proxy_set_header Forwarded "for=$remote_addr;proto=https;host=downloads.example.internal";
    proxy_buffering off;
    proxy_read_timeout 600s;
}
```

将实际代理网段写入 `trusted_proxies`，并在反向代理配置域名及访问策略。仅可信 peer 的转发头生效；合法 Forwarded 优先于 X-Forwarded-*，沿链从右向左选择信任边界。多值 Host/Proto 必须与 XFF 长度一致，单值须由直连可信代理覆盖。畸形可信 origin 返回 400，不混用两套头。代理必须删除或覆盖客户端转发头，不应信任覆盖公网客户端的 CIDR。

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

`/health/live` 不依赖上游；`/health/ready` 检查 SQLite 和目录可写，不要求外网在线。CLI healthcheck 按相同的路径/字段优先级解析配置，连接实际监听地址并使用允许的 Host，不受公共发布地址改变影响。健康请求不计入业务访问。

当前 active 采集目录为 41 项。16 个常用/25 个诊断前端分组及图表 tooltip 已整合。`reuse_requests` 不再独立写入/采样/展示；`events.recent_total` 不再采样/展示，事件详情保留。全局 versions.total 计算全部规范应用的版本记录。旧样本（若新格式库已有）自然过期，不通过未知 ID 删除历史；本次架构切换本身不导入旧库历史。

默认每分钟采样，每小时聚合；24h 使用分钟点，7d/30d 使用小时汇总，保留期分别 24h/30d。缺失点留空、当前未完整小时标记 partial；计数器不平均累计量，也不把重启或断档视为零增量。磁盘 used 为已分配块，free 为文件系统可用空间，清理释放累计为逻辑字节。计数和磁盘状态不是每字节原子事务，完整性校验与发布不依赖统计。单个版本/资源不扩展成无限历史标签。

## 上游与安装器边界

协议适配器校验版本、渠道、metadata、签名和授权制品；共享分发层仅允许对应编译期上游 origin 和路径，限制重定向并禁止直接连接私有/loopback/link-local DNS 地址。新增 CDN 或信任根须审查代码，失败时不自动猜测来源。

安装器原文、patch、generated、provenance 和许可分离保存；统一 descriptor 驱动产物清单和固定验证器。更新须通过严格 patch、隔离离线检查及宿主重验，daily PR 不可顺带改 descriptor、信任根或验证代码。详见[安装器维护](installers-maintenance.md)。

安装器下载经本服务，不改写应用运行期/API 流量。抑制安装器更新标记不等于禁用全部 CLI 更新检查；Windows/PowerShell、macOS、官方真实制品和生产下载链仍需独立上线验证。
