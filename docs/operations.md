# RedApp 运维说明

本文对应本地 0.7.2（未发布）。SQLite schema=6，精确 v0.7.0 schema 4 / v0.7.1 schema 5 在独占锁下按版本事务升级，保留已有数据；更早或未知目录在写入前拒绝。升级前停止实例并备份完整目录；已有厂商 ID `all` 会明确报冲突，不自动改名或删除。部署配置 schema_version 仍为 1。详见 [0.7.2 运行变更](admin-experience-v0.7.2.md)，缓存规则历史参考见 [v0.7.0 运行说明](provider-runtime-v0.7.0.md)。

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

`max_writers` 和 `max_readers` 都是全应用共享的并发上限。发布下载的 writer 槽位覆盖回源、重试等待和校验；HTTP Cache 回源及 HEAD 同样占用 writer。reader 包含等待下载或客户端读取的请求，缓存命中、HEAD 和 304 仍占 reader。可共享响应的跟随者不各占一个 writer；不可共享响应独立回源。超限请求返回 503，不进入容量等待队列。

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

Info、Hosted、HttpCache、Codex、ClaudeCode 是编译期 Provider，通过组合复用传输和全局容量限制；应用 BaseUrl 可在后台配置，不加载运行时插件。Codex/ClaudeCode 保留受审查的元数据、签名或摘要验证规则。`REDAPP_PUBLIC_URL` 不属于上述字段覆盖链，只给后台公共 URL 设置提供环境默认值；文件/CLI 不增加新的 public_origin 来源。

站点文案、回源代理和公共地址覆盖保存在全局 settings；应用 BaseUrl 与 TTL 保存在动态应用记录。发布 Provider 的渠道 TTL 默认 60 秒、范围 1–86400；HTTP Cache 默认 300 秒、范围 0–86400，0 表示每次重新验证。应用创建时 revision 从 1 开始。更新校验读取时的 revision，冲突返回 409；应用资料、TTL 和启用状态共享应用 revision，不能用旧表单覆盖新设置。

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

`GET/PUT /admin/api/settings/proxy` 管理共享出口代理，PUT 要求 revision 和会话/CSRF。server 为带端口的 `http://host:port`、`https://host:port` 或 `socks5://host:port`；URL 可包含百分号编码的用户名/密码，禁止路径、查询、fragment，空 server 表示直连。不继承 HTTP_PROXY/HTTPS_PROXY。HTTP 代理以 CONNECT 访问 HTTPS 上游，SOCKS5 由代理端解析 DNS；应用配置的路径边界、Provider 重定向规则和基本 TLS 校验仍生效。

配置仅保留 `server` 完整 URL；GET 返回原始完整 `server`、`dns`、`revision`，仅受保护后台可读。旧 `username/password` 在首次加载时编码进 URL 并 CAS 保存一次，后续不保留独立凭据字段或动作。空 server 清除代理。公共响应、日志与事件不包含代理凭据；SQLite 和完整备份按敏感材料保管。

代理先 CAS 持久化，再切换所有应用共享的 transport；失败不生效。新请求使用新配置，已开始传输的响应自然结束。此操作不强制失效 metadata、缓存制品或正在进行的摘要校验。

## Vendor/App 与可变 HTTP 缓存

`/admin/vendors` 管理全小写厂商 ID、中英文名称/描述和图标，再在厂商下创建应用并选择 Provider。启动补齐缺失内置模板且默认禁用，已有完整键记录不覆盖。ID、隶属和 Provider 固定；内置完整键应用不可删除。自定义应用确认后永久删除并释放公开 ID，重新创建使用新内部 UID；仍有任何应用的厂商不可删除。模板重置先选字段、预览差异，再通过 CAS 保存，不删除文件或历史。

启用状态同时受厂商与应用控制。禁用厂商不会覆盖应用自身开关；禁用停止新的公开请求并保留数据；永久删除清除该应用拥有的缓存、文件和历史，全局共享图标独立保留。删除要求共享传输池空闲；物理清理回执持久化，可在重启时继续。HTTP Cache 支持 1–16 个有序 `base_urls`，以及 `ordered`、`round_robin`、`random` 策略；列表内容、顺序和策略改变都会创建新 source epoch，不搬运旧缓存。发布 Provider 仍为单 BaseUrl。已进入的请求可结束，但旧 revision 的写入不能成为新缓存头；历史 source 可显式选择并清理。

HTTP Cache 路径位于 `/<vendor>/<app>/<relative-path>`。TTL 只决定新鲜度，不等于磁盘保留期；优先级为首次命中的路径规则、源 Cache-Control 寿命、完全没有 Cache-Control 时的应用默认值；有 Cache-Control 却无有效寿命按 TTL 0 每次验证。304 只刷新验证时间，不伪造获取时间。TTL 0 仍保留完整副本供失败回退。

手动清理组合 `match={type:glob|re2,pattern}`、`basis=fetched_at|last_access`、带时区的 `before` 及可选 `source_epoch`。清理与刷新共用服务端分页冻结预览：默认每页 25、最多 100，执行整个集合而非当前页；构建/执行分批短事务。自动清理默认空规则，每 15 分钟处理活动当前来源，每应用每轮最多扫描 1000、退休 100。第一条路径匹配规则拥有文件，即使年龄未到也不继续下一条。最后访问按分钟桶持久化，执行重新检查访问及配置 revision。完整匹配示例、刷新 API、上限和故障行为见 [Provider 运行说明](provider-runtime-v0.7.0.md)。

GET 支持完整响应、条件请求和单段 Range，多段 Range 忽略后返回完整响应。可缓存冷请求先完整落盘，再开始服务，包括冷 Range 请求，因此首字节可能等待整个回源；跨镜像完整重取，不拼接半截文件，验证器绑定实际来源。HEAD 无正文，冷 HEAD 只向上游发 HEAD。未被显式规则覆盖的 no-store/private，以及 Set-Cookie、不支持的 Vary 等不能共享的响应独立直接传输且受大小限制。

网络、超时、上游 5xx 时按策略尝试后续镜像；每源最长 5 分钟，整次回源预算 9 分钟。可尝试源全部失败后，按每 App `stale_fallback` 开关决定是否返回已有同应用、同 epoch 的完整副本；默认 true，false 返回错误。无额外 stale 年龄上限，不受源重验证指令禁止 stale 的限制；回退不推进 fetched_at/validated_at。404/410 不视为临时故障。显式规则可有意覆盖 no-store/private 供公开分发复用，正 TTL 真实覆盖时记录警告，TTL 0 不记覆盖警告；真实回退另记一次共享回源警告，cache hit 无警告，不新增限频。发布验证和表示隔离边界不受影响，更多细粒度控制仍见 [backlog #2](https://github.com/PMExtra/RedApp/issues/2)。

图标仅接受实际可解码 JPG/PNG 或静态 SVG 子集；上传上限 2 MiB、栅格边长上限 4096、像素上限 4 Mi，拒绝活动 SVG 内容或外部引用。图标按独立资源返回，正文不插入管理页执行。HTTP Cache 文件也以附件返回；不托管活动 HTML/SVG 页面。

## 数据目录、清理与恢复

- `instance.lock` 是保留的内核锁文件；进程退出或崩溃后内核释放锁，禁止人为删除锁 inode。获取写锁前先以只读方式检查已有目录，拒绝旧 schema 或未知内容，不创建锁来污染被拒绝的旧目录。
- SQLite schema=6，包含应用说明、Hosted 持久文件、小时去重 sketch、首页置顶和待清理回执，包含 Vendor/App、历史 source snapshots、全局设置、发布 metadata/渠道/资源/generation/blob、HTTP 缓存、指标、事件、清理快照和管理员记录。没有迁移命令；schema=2、schema=3 和未知目录均在写入前被拒绝。
- 应用有稳定内部 UID；BaseUrl 变更创建新的 source epoch。发布逻辑资源身份为 `(app_uid,source_epoch,version,resource_key)`，完整 blob 仅在同一 source namespace 内按摘要复用，不跨应用/epoch 复用。每次下载拥有独立随机 generation。未完成文件位于 `objects/parts/<generation>.part`，完整文件位于 `objects/blobs/<app摘要>/<内容摘要>.blob`，URL 不直接映射磁盘路径。
- 活动下载仅按精确逻辑资源合流。完整校验、fsync 和文件发布后才能标记 complete；重启核对磁盘和数据库，损坏/缺失文件不能作为已验证缓存返回。续传使用强 ETag/If-Range 并验证范围、编码、长度和最终摘要。
- 清理预览冻结指定应用/source epoch 的精确 generation 集合及 App/Vendor revision，有效 10 分钟；执行不能跨应用、不能扩大到新 epoch，配置改变需要重新预览。成功回执支持重试。旧代退出当前状态后等待已有读写租约排空；应用内共享 blob 只在最后引用结束后回收。版本发现、可信 metadata 和累计指标不随缓存清理删除。
- 一个本地目录只由一个实例使用，不支持 NFS/SMB。Docker 可采用只读根文件系统加可写持久卷；镜像内 `/var/lib/redapp` 为 UID/GID 65532、模式 0700。已有宿主 bind mount 的权限需管理员预先设置，不递归自动 chown。

备份先正常停止服务，再复制整个新格式数据目录，包括可能存在的 WAL/SHM。恢复到同格式目录前确认没有服务持锁，不在线单独复制 state.sqlite。只有精确匹配的 schema 4/5 原地升级；更早或未知格式不导入配置、缓存或历史。升级后不能用旧二进制打开 schema 6；回退须使用完整升级前备份。

## 路由、管理 API 与指标

首页 `/` 为置顶与排行，全部目录为 `/all`，厂商详情为 `/<vendor>`；应用详情为 `/<vendor>/<app>`，制品及 installer 位于 `/<vendor>/<app>/<file_path>`，启动补齐的内置模板须管理员启用后公开。`all`、`admin`、`api`、`assets`、`health` 为保留命名空间。旧 `/apps/codex`、根 `/install.sh` 和 `/api/info` 不提供兼容别名。`GET /api/bootstrap` 返回公开站点、应用定义和公共地址，不依赖管理 status。

后台页面有真实路径：`/admin/overview`、`/admin/events`、`/admin/settings/site`、`/admin/settings/proxy`、`/admin/vendors`、`/admin/vendors/<vendor>/apps/<app>/versions`、`.../files`、`.../cache`、`.../settings`，可以刷新和直接打开。设置页不订阅全局 status 轮询。

| API | 用途 |
| --- | --- |
| `POST /admin/api/login`、`/logout`、`/password` | 登录、退出、改密码 |
| `GET /admin/api/session`、`/status`、`/events` | 会话、全局状态、事件 |
| `GET/PUT /admin/api/settings/site`、`.../proxy`、`.../public-url` | 带 revision 的全局设置 |
| `GET /admin/api/providers` | 固定 Provider 定义、默认值与能力 |
| `GET/POST /admin/api/vendors`、`GET/PATCH/DELETE /admin/api/vendors/<vendor>` | 厂商列表、创建、资料与启用状态、删除 |
| `POST /admin/api/vendors/<vendor>/apps`、`GET /admin/api/apps`、`GET/PATCH/DELETE /admin/api/apps/<vendor>/<app>` | 动态应用管理；列表服务端分页；ID、隶属、Provider 固定 |
| `GET/PUT /admin/api/settings/homepage` | 有序置顶完整键列表与 revision CAS |
| `GET/POST /admin/api/apps/<vendor>/<app>/template`、`/admin/api/vendors/<vendor>/template` | 兼容字段分组、当前值/模板值与选择性 CAS 重置 |
| `GET /api/home`、`/api/catalog`、`/api/search`、`/api/vendors/<vendor>` | 有效启用目录、排行、分页搜索与建议 |
| `GET /api/apps/<vendor>/<app>/instructions/document?lang=en` | 独立策略的受信任 HTML 说明文档 |
| `POST /admin/api/assets/icons` | 单个 multipart JPG/PNG/静态 SVG 图标 |
| `GET/PUT /admin/api/apps/<vendor>/<app>/settings` | 发布 Provider 的 channel_ttl_seconds 与应用 revision |
| `GET/PUT /admin/api/apps/<vendor>/<app>/instructions` | 独立 revision 的双语 Markdown/HTML/JavaScript 说明 |
| `GET/POST/DELETE /admin/api/apps/<vendor>/<app>/files...` | Hosted 分页、上传/一次性导入、进度/取消和明确删除，见 0.7.1 运行变更 |
| `GET /admin/api/apps/<vendor>/<app>/sources` | 当前与历史 source epoch |
| `GET /admin/api/apps/<vendor>/<app>/cache` | HTTP Cache 文件缓存，支持 source_epoch 选择 |
| `POST /admin/api/apps/<vendor>/<app>/cache/cleanup/preview`、`.../<id>/execute` | 按 fetched_at 或 last_access 的 before 时刻预览/执行 |
| `GET /admin/api/apps/<vendor>/<app>/status`、`.../versions`、`.../resources` | 明确应用状态 |
| `POST /admin/api/apps/<vendor>/<app>/cleanup/preview` | minimum_version 清理预览 |
| `POST /admin/api/apps/<vendor>/<app>/cleanup/<id>/execute` | 执行冻结预览 |
| `GET /admin/api/history?scope=global&metric=...&range=24h` | 全局指标历史 |
| `GET /admin/api/apps/<vendor>/<app>/history?metric=...&range=24h` | 有限应用维度历史 |

不存在默认 Codex 应用，也不接受应用选择 header 作为身份替代。资源 path、query、应用和授权均须通过服务端校验，未知 API 不回退成成功 HTML。

status 仅返回摘要和指标，列表从 versions、resources、events 单独读取。版本/资源在新界面通过 `page` 参数使用 `items,page,limit,total,total_pages` 的编号分页，默认 25、最大 100；越界自动夹到末页，不能同时携带 cursor。事件和旧游标 API 的列表响应为 `{"items":[],"next_cursor":null}`；`limit` 默认 50、最大 100，下一页提交返回的非空 `cursor`。resources 可用 `version` 精确筛选。游标绑定应用、端点和筛选条件，不能在切换应用或版本后复用；版本按文本升序、资源按 generation ID 升序、事件按 ID 降序。分页是实时视图，不承诺跨请求冻结快照。

`/health/live` 不依赖上游；`/health/ready` 检查 SQLite 和目录可写，不要求外网在线。CLI healthcheck 按相同的路径/字段优先级解析配置，连接实际监听地址并使用允许的 Host，不受公共发布地址改变影响。健康请求不计入业务访问。

当前 active 采集目录为 41 项。16 个常用/25 个诊断前端分组及图表 tooltip 已整合。`reuse_requests` 不再独立写入/采样/展示；`events.recent_total` 不再采样/展示，事件详情保留。应用指标使用稳定 UID，换源不拆分累计历史；版本指标仅适用于发布 Provider，HTTP Cache 不伪造版本。旧样本（若新格式库已有）自然过期，不通过未知 ID 删除历史；本次架构切换本身不导入旧库历史。

默认每分钟采样，每小时聚合；24h 使用分钟点，7d/30d 使用小时汇总，保留期分别 24h/30d。缺失点留空、当前未完整小时标记 partial；计数器不平均累计量，也不把重启或断档视为零增量。磁盘 used 为已分配块，free 为文件系统可用空间，清理释放累计为逻辑字节。计数和磁盘状态不是每字节原子事务，完整性校验与发布不依赖统计。单个版本/资源不扩展成无限历史标签。

## 上游与安装器边界

Codex/ClaudeCode 适配器继续校验版本、渠道、metadata、原有签名或摘要以及授权制品。BaseUrl 可以覆盖为兼容的 HTTP(S) 企业源；请求者只能提供相对路径，不能用 URL 参数选择任意目的地址。HTTP Cache 支持有限的 HTTPS 跨源重定向和 HTTP→HTTPS 升级，禁止 HTTPS 降级并剥离跨源凭据；发布 Provider 仍限制到配置的源和路径。新源认证、私有 CA、可配置信任/签名与高级安全机制不在本版范围；不提供跳过证书验证开关。相关增强记录在 [backlog #1](https://github.com/PMExtra/RedApp/issues/1)，这不删除现有后台认证、CSRF、签名或摘要校验。

安装器原文、patch、generated、provenance 和许可分离保存；统一 descriptor 驱动产物清单和固定验证器。更新须通过严格 patch、隔离离线检查及宿主重验，daily PR 不可顺带改 descriptor、信任根或验证代码。详见[安装器维护](installers-maintenance.md)。

安装器下载经本服务，不改写应用运行期/API 流量。抑制安装器更新标记不等于禁用全部 CLI 更新检查；Windows/PowerShell、macOS、官方真实制品和生产下载链仍需独立上线验证。
