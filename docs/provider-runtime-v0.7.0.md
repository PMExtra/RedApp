# v0.7 Provider 运行说明

本文描述 v0.7.0 的 Provider 运行方式。默认监听仍是 `:8080`。启动配置、公共 URL 与代理设置见[运维说明](operations.md)；下文说明路径规则、新鲜度优先级、故障回退和清理边界。

## 数据目录与升级边界

SQLite schema 为 **4**；部署 YAML/JSON 的 `schema_version` 仍为 **1**。v0.6 使用的数据库 schema=3 也属于旧格式，不能直接复用。为 v0.7 选择全新的空目录/卷，不把旧库、缓存、后台设置或历史复制进去。没有迁移工具，也没有自动导入。

启动先只读检查目录，旧 schema 或未知内容会导致失败；不会用创建锁文件、升级数据库或删除内容的方式修复旧目录。旧目录应由管理员保留，作为旧版本的独立存档。同格式 v0.7 数据可以正常重启。备份须先停止持锁的实例，再复制完整目录，包括可能存在的 SQLite WAL/SHM。

尚未发布的早期 v0.7 快照即使同标为 schema 4，只要表结构与本快照不同也会拒绝；不会静默补列或导入。首次运行本快照仍应使用新的空目录。

一个本地目录只由一个实例使用，不支持多个进程共享写入或 NFS/SMB。部署配置的字段优先级和权限要求不变。

## 三个并列 Provider

| Provider/API key | BaseUrl | 主要能力 |
| --- | --- | --- |
| GeneralHttp / `general-http` | 必填，1–16 个有序来源 | HTTP 文件分发、可变缓存、刷新及按规则清理 |
| Codex / `codex` | 默认 `https://releases.openai.com/codex`，可覆盖 | Codex 版本、渠道、可信 metadata、摘要授权与安装器 |
| ClaudeCode / `claude-code` | 默认 `https://downloads.claude.ai/claude-code-releases`，可覆盖 | Claude Code 版本、渠道、原有签名/摘要校验与安装器 |

Provider 是编译进单进程 Go 服务的实现，通过组合复用 HTTP 传输、出口代理与全局容量限制，不是运行时插件。GeneralHttp 的本地 SHA256 用于检测存储损坏，不代表上游发布者签名。Codex/ClaudeCode 继续使用各自已有的授权与校验流程，选择企业镜像不会把任意文件变成可信发布。

BaseUrl 接受 HTTP(S) 地址、显式端口和目录路径，支持企业内网源；不接受 URL 内凭据、query 或 fragment。HTTPS 保留系统信任根的证书验证。本版没有新源认证、自定义私有 CA、可配置签名密钥或跳过证书验证开关。更高级的企业源与安全机制见 [issue #1](https://github.com/PMExtra/RedApp/issues/1)，不应据此移除现有后台认证、同源/CSRF、签名或摘要保护。

新数据目录仅初始化一次 `openai/codex` 与 `anthropic/claude-code`。之后可以添加同类 Provider 的独立应用实例；重启不会覆盖配置、重新启用应用或恢复已删除的种子。目录可以没有任何活动应用。

### GeneralHttp 多源

GeneralHttp 的 `base_urls` 为有序非空列表，最多 16 项，规范化后不允许重复。`source_strategy` 支持 `ordered`（默认，依次回落）、`round_robin`（轮转起点后遍历其余来源）和 `random`（每次随机排列，不重复尝试同一项）。管理页支持拖拽以及键盘可用的上下移按钮。Codex/ClaudeCode 仍使用单个 `base_url`；多源能力不改变发布验证。

只有真正回源才选择来源，新鲜缓存命中不推进轮询。网络失败、超时或 5xx 才会尝试后续来源；404/410 为终局响应，其他 4xx、TLS/重定向/编码校验错误不当作可重试的网络错误。每源保留 5 分钟上限，整次回源预算为 9 分钟，预算耗尽后不再尝试剩余项，再按 `stale_fallback` 决定是否返回旧缓存。没有主动健康检查、权重或熔断。轮询状态保留最多 4096 个最近使用应用，重启或淘汰会重置起点。

列表内容、顺序或策略变化都原子创建新的 source epoch；名称、TTL 和规则编辑不换 epoch。每个缓存 generation 记录实际响应的完整来源 URL（不含凭据或 query），只有与本次初始请求 URL 完全一致时才复用验证器。GeneralHttp 重定向会剥离验证器和 Range，304 还需核对最终 URL 与实际发送的条件；重定向对象通常完整重取，这是隔离不同表示的保守取舍。下载中断后跨源重新开始，不拼接不同镜像的半截内容。即使 ETag 和长度相同，跨源表示仍不具有全局相同性保证；GeneralHttp 没有可信预期摘要，故本轮不实现跨源 Range 续传，参见 [RFC 9110 §8.8.1](https://www.rfc-editor.org/rfc/rfc9110.html#section-8.8.1)。已开始发送的独享直传响应失败时也不会透明换源混包。

## 厂商、应用与身份

在 `/admin/vendors` 创建厂商，再在厂商下创建应用。厂商和应用都包含 `id`、`name`、`description`、`icon`、`enabled`；名称和描述使用 `en`、`zh-CN` 两个语言键。名称两种语言均必填，描述可为空。

ID 使用 1–63 字符的小写字母、数字和单个连接符分段，不接受大写、空白或路径分隔符。厂商 ID 全局唯一，应用 ID 在厂商内唯一；`admin`、`api`、`assets`、`health` 是保留厂商名。应用公开身份为 `<vendor>/<app>`。本版不修改 ID、隶属厂商或 Provider，不创建旧名称别名。

创建后可调整资料、图标、BaseUrl、TTL 和启用状态。应用与厂商各自带 `revision`；修改、删除提交读取时的 revision，冲突返回 409，重新读取后再决定是否保存。应用资料、TTL、路径缓存规则、故障回退开关和自动清理规则使用同一个应用 revision。

启用要求厂商与应用同时开启；关闭厂商不会改写应用自身的开关。禁用和删除停止新的公开访问，已经进入的请求可以结束。删除是保留数据的标记操作：不清空缓存、历史或图标，ID 继续保留。删除厂商前须先删除其未删除的应用。

## 换源、历史缓存与清理

应用有稳定内部 UID；更改 BaseUrl 时创建独立的 source epoch。新请求只读取当前 epoch，不把旧源的缓存套到新源；累计指标继续归属于同一个应用。资料或启用状态变更也更新 revision，延迟完成的旧下载/metadata 不能越过围栏发布新的当前缓存。

旧 epoch 的缓存留在磁盘，可由管理员显式选择。`GET /admin/api/apps/<vendor>/<app>/sources` 返回 epoch、BaseUrl、当前/活动状态和创建时间；清理入口支持 `?source_epoch=<epoch>`。服务端检查 epoch 属于该应用，禁用或删除不妨碍显式的缓存管理。

发布 Provider 按 `minimum_version` 预览；GeneralHttp 组合路径模式、获取时间或最后访问时间及截止时刻预览：

```json
{"match":{"type":"glob","pattern":"/releases/"},"basis":"fetched_at","before":"2026-01-01T00:00:00Z"}
```

GeneralHttp 预览入口为 `POST /admin/api/apps/<vendor>/<app>/cache/cleanup/preview`，`basis` 可取 `fetched_at` 或 `last_access`，`before` 必须携带时区且不能在未来；省略 `match` 表示 glob `/`，即全部文件。执行入口为 `POST .../cache/cleanup/<id>/execute`。发布 Provider 继续使用 `.../cleanup/preview` 和 `.../cleanup/<id>/execute`。

预览冻结匹配模式、时基、截止时间和精确 generation 集合，有效 10 分钟；执行不扩大集合，配置 revision 变化要求重新预览。最近访问清理还会复查预览后的访问，避免删除刚被使用的缓存。清理逻辑上退出当前缓存，物理文件等正在使用的读写租约结束后回收。TTL 不是保留期，是否自动清理由下述独立规则决定。

### 分页预览与手动刷新

清理和刷新共用持久化预览及逐项结果。预览只返回总文件数、总字节和 token；`GET .../cache/{cleanup|refresh}/<id>/items` 使用服务端游标分页，默认 25 项、最多 100 项。页面不先取全量再分页；执行针对完整冻结集合，与当前浏览页无关。

构建预览按最多 1000 行的短事务推进，以开始时的单调 generation 高水位排除新插入文件；构建过程中已替换或移除的文件可以不进入最终集合。完成后集合固定，执行逐项复查 generation、访问时间和配置围栏；它不是跨所有批次的数据库瞬时快照。清理每批最多 100 项，刷新每页最多 25 项，无遍历全量资源的大 JSON 或贯穿网络操作的数据库事务。

最多同时构建 8 个预览，超出返回忙状态。过期集合按最多 1000 项的短事务逐步回收；构建前、定时清理和启动都会执行有界回收。重启将未完成的构建/执行标记失败，不悄悄继续旧任务；已执行结果保留供查询，新操作需重新预览。

`POST .../cache/refresh` 接收 `{"path":"/releases/tool.zip"}`，强制验证一个现有资源。批量使用 `POST .../cache/refresh/preview` 接收 `{"match":{"type":"glob","pattern":"/releases/"}}`，再 `POST .../cache/refresh/<id>/execute` 启动后台执行；`GET .../cache/refresh/<id>` 查看状态，逐项结果仍分页读取。只刷新活动应用当前 epoch 中已存在的缓存，不爬取未知上游路径；历史来源只查看和清理。

单个串行批量刷新 worker 复用全局 reader/writer 配额，后台执行不依赖浏览器连接。304 更新验证时间而不更改获取时间；200 完整提交后才替换旧 generation。逐项结果区分 `refreshed`、`not_modified`、`stale_fallback`、`failed` 和 `skipped`；使用旧缓存不等于刷新成功。配置变化或服务停止会终止剩余工作，不复活已清理或禁用的资源。完成回执保留 24 小时，重复执行读取回执；运行中的重复请求返回冲突。

`fetched_at` 表示完整正文提交的时间，304 只刷新验证与新鲜度，不伪造一次新的获取。成功的缓存 GET/HEAD/304 记录最近访问，按分钟桶持久化并使用桶结束时间作为保守清理边界；尚无访问记录时，最近访问清理使用获取时间。页面显示的访问时间因此不是逐请求精确时间戳。

## GeneralHttp 路径规则与缓存策略

仅 GeneralHttp 在应用设置中提供此策略。每应用保存 `rules: [{id, match, ttl_seconds}]`、`auto_cleanup: [{match, basis, age_seconds}]` 和 `stale_fallback`；默认规则列表均为空，`stale_fallback=true`。新缓存规则可省略 `id`，保存时生成；读取返回的 ID 应随该规则后续编辑保留。不存在 `mode` 或 `bypass` 字段。

```json
{
  "stale_fallback": true,
  "rules": [
    {"match":{"type":"glob","pattern":"/releases/"},"ttl_seconds":3600},
    {"match":{"type":"re2","pattern":"/channels/[^/]+\\.json"},"ttl_seconds":0}
  ],
  "auto_cleanup": [
    {"match":{"type":"glob","pattern":"/releases/"},"basis":"last_access","age_seconds":2592000}
  ]
}
```

路径为**解码后、以 `/` 开头的应用相对路径**，不包含 vendor/app、BaseUrl 前缀或查询参数。例如 `/example/files/releases/tool.zip` 的匹配输入是 `/releases/tool.zip`。缓存规则、手动清理、自动清理和匹配测试共用相同的 `pathmatch` 实现，不读取或遍历操作系统目录。

| 模式 | 匹配含义 |
| --- | --- |
| `{"type":"glob","pattern":"/"}` | 所有合法应用文件路径 |
| glob `/releases` | 精确的 `/releases` 文件，或 `/releases/...` 目录子树 |
| glob `/releases/` | 仅该目录的后代文件，不把精确的 `/releases` 文件算入 |
| glob `/**/*.zip` | 使用 doublestar/v4 的 slash-separated glob；可匹配完整文件路径或目录祖先 |
| re2 `/releases/[^/]+\.zip` | Go RE2 完整路径匹配；不自动匹配子树或子串 |

glob 可使用 `*`、`**` 等 doublestar/v4 语法。目录后代语义意味着：模式命中某个目录祖先时，该目录内的文件也匹配。RE2 采用全串匹配，不支持 Go regexp 不接受的反向引用或 lookaround。两种列表都按保存顺序选择**第一条路径命中**；自动清理若第一条已命中但年龄未达到，不继续尝试后面的规则。调整顺序会改变行为。

新鲜度采用以下优先级，而不是将全部数值取最小值：

1. **匹配缓存规则：**使用规则的 `ttl_seconds`，从验证时间计算，不扣源 Age/Date，不受应用默认 TTL 上限限制；覆盖源 Cache-Control 中的缓存寿命和存储限制。
2. **未匹配、源存在 Cache-Control：**优先有效 `s-maxage`，否则有效 `max-age`，扣除源 Age/Date 反映的年龄；应用默认值不再封顶。存在 Cache-Control 却没有有效寿命时按 TTL 0 处理，重复或非法寿命值保守视为到期。
3. **未匹配、源完全没有 Cache-Control：**使用应用默认 `cache_ttl_seconds`，默认 300 秒，从验证时间计算，不扣 Expires/Age。Expires 不增加另一层优先级。

`no-cache`、`must-revalidate`、`proxy-revalidate` 及 `s-maxage` 附带的重验证要求不额外强制验证或禁止 stale；例如 `max-age=600, no-cache` 的新鲜期仍按有效寿命计算。ETag/Last-Modified 条件验证保持启用。所有 TTL 均为非负秒数；**TTL 0 每次先回源，仍可保存完整正文供故障回退**，不等同于不保存文件。

显式路径规则可覆盖 `no-store/private`，包括 TTL 0；未命中规则时，它们仍不存储、不共享。对 `private` 的覆盖意味着管理员有意在公开下载端复用该表示。Set-Cookie、不支持的 Vary、请求自身的 no-store 和认证表示隔离边界不因此放宽。正 TTL 规则只有在真实回源覆盖源策略时才记录覆盖警告；每次回源一次，缓存命中不记，TTL 0 抑制覆盖警告。警告不去重、不限频。

策略保存使用 `PUT .../cache/policy` 和 `If-Match: "<应用 revision>"`，成功后返回新的应用 revision。保存本身不改 source epoch、不删除现有正文；命中判断和列表新鲜度使用当前策略，旧 revision 的 writer 不能发布新缓存头。策略保存与其他应用设置共同参与 CAS，过期表单返回 409。

| 输入 | 限制 |
| --- | --- |
| 策略请求正文 | 128 KiB |
| 缓存规则 / 自动清理规则 | 各最多 32 条 |
| 单个 pattern | 1–1024 UTF-8 字节 |
| `ttl_seconds`、GeneralHttp 应用默认 TTL | 0–86400 秒 |
| `age_seconds` | 60–315360000 秒 |

### 自动清理

`auto_cleanup=[]` 默认不启用。保存规则后由单个串行调度器每 15 分钟检查一次，不在服务启动时立即删除。只处理活动 GeneralHttp 应用的当前 source epoch；禁用、删除或旧 epoch 的缓存留给显式手动清理。

每应用每轮最多扫描 1000 个当前文件、最多退休 100 个命中且达到年龄的 generation。使用继续扫描的游标，走到末尾后从头开始，避免前面的条目长期占满扫描额度。配置 revision 变化会重置对应游标并重新校验规则；失败不推进该应用游标，下轮重试，同时继续其他应用。

自动清理复用手动清理的冻结选择、最近访问复查、reader pin、App/Vendor revision 和 source epoch 约束。文件有使用中的租约时延迟物理回收，不绕过这些保护。失败记录事件及自动清理状态；状态包含最近尝试/成功/失败、扫描数、退休数和累计失败数，可在 GeneralHttp 页面查看。该状态描述共享调度器，不是新增长期逐文件历史。

## GeneralHttp 的 HTTP 行为

`/<vendor>/<app>` 是详情页，后面的相对路径映射到 BaseUrl 目录下，例如 BaseUrl 为 `https://files.example.internal/tools` 时，`/example/files/builds/tool.zip` 回源到 `/tools/builds/tool.zip`。首版拒绝查询参数、路径穿越、编码后的路径分隔符与控制字符；请求者不能指定任意目标 URL。

| 情况 | 行为 |
| --- | --- |
| 可缓存的冷 GET | 完整回源到受限磁盘缓存后才开始响应；未知长度也受单文件上限约束 |
| 新鲜缓存 | 从已完成的正文服务 GET/HEAD 和条件请求，不重复回源 |
| 过期缓存 | 使用上游 ETag/Last-Modified 重新验证；304 刷新验证时间，200 创建新 generation |
| 冷 HEAD | 只向源发 HEAD，不触发完整 GET、不建立空正文缓存 |
| 单段 Range | 基于完整正文返回 206；冷缓存仍先获取完整文件，不建立稀疏分段缓存 |
| 多段 Range | 忽略多段请求并返回完整表示，不生成 multipart ranges |
| 未被显式规则覆盖的 no-store/private，或 Set-Cookie、不支持的 Vary、请求本身不能共享 | 独享有大小限制的直接传输，不落盘共享正文，也不把该响应分给其他等待者 |
| 网络失败、超时或上游 5xx | `stale_fallback=true` 且有可用完整过期缓存时回退，不加最大 stale 年龄；关闭或没有缓存则失败 |
| 404/410 | 返回对应状态，不静默解释为可用 stale 的上游故障 |

文件以 `application/octet-stream` 附件返回，设置 nosniff 和限制性 CSP；不作为管理页面同源下的活动网站内容执行。不能共享的响应开始传输后若断流，响应失败，不能中途拼接旧缓存。

GeneralHttp 允许有限的 HTTPS 跨源重定向和 HTTP→HTTPS 升级，禁止 HTTPS 降级，跨源剥离凭据。发布 Provider 继续限制到配置的源与路径。请求 Cookie、Authorization 或自定义目标 Host 不转发给回源地址。所有 Provider 共用 reader/writer 与文件大小上限，HEAD 回源也占 writer；上限不是每应用各自一套配额。

GeneralHttp 的应用默认 TTL 为 300 秒、允许 0–86400，只有无匹配规则且无 Cache-Control 时使用，不是所有文件的 TTL 上限。发布 Provider 的渠道 TTL 默认 60 秒、允许 1–86400；已授权的不可变版本不因渠道 TTL 到期而变成可变文件。

## 图标与管理 API

`POST /admin/api/assets/icons` 接受一个 multipart 图标文件，返回可用的 `icon` 路径，再把该路径保存到厂商/应用。上限为 2 MiB，支持实际可解码的 JPG/PNG；栅格边长最多 4096、最多 4 Mi 像素。SVG 只接受静态图标子集，拒绝脚本、事件、外部引用和不支持的活动元素，不执行任意上传 XML/HTML。

常用目录 API：

- `GET /admin/api/providers`：固定 Provider 默认值与能力。
- `GET/POST /admin/api/vendors`，`GET/PATCH/DELETE /admin/api/vendors/<vendor>`：厂商管理。
- `POST /admin/api/vendors/<vendor>/apps`，`GET /admin/api/apps`，`GET/PATCH/DELETE /admin/api/apps/<vendor>/<app>`：应用管理。
- `GET /admin/api/apps/<vendor>/<app>/cache`：GeneralHttp 缓存列表；发布 Provider 保留 versions/resources 入口。
- `GET/PUT /admin/api/apps/<vendor>/<app>/cache/policy`：有序缓存规则、自动清理规则和 `stale_fallback`；读取返回应用 `revision` 与 ETag，保存通过 `If-Match` 校验。
- `POST /admin/api/apps/<vendor>/<app>/cache/match`：用 `{"match":{"type":"glob","pattern":"/releases"},"path":"/releases/tool.zip"}` 测试匹配，返回 `matches` 与 `canonical_path`，不执行清理。
- `GET /admin/api/apps/<vendor>/<app>/cache/cleanup/status`：共享自动清理调度器状态。

目录创建不需要 revision；PATCH/DELETE 提交 `If-Match: "<revision>"` 或 body 中的 `revision`，两者同时提供时必须一致。目录响应是 `{"vendor":...}` 或 `{"app":...}`，并不沿用全局设置的扁平响应。所有管理写入继续使用现有会话、Origin 与 CSRF 校验。

## 已确认的故障回退策略与当前限制

**`stale_fallback` 默认开启：网络、超时或上游 5xx 导致验证失败时，已有同应用、同 source epoch 的有效完整缓存就返回旧内容。关闭时返回错误，不使用过期缓存。** 这是明确选择的镜像可用性策略，覆盖源禁止 stale 的约束，不宣称严格透明的 HTTP 缓存行为。正常时按照上述 TTL 优先级判断新鲜度；失败回退不更新 fetched_at/validated_at、不增加 stale 年龄上限，也不会停止后续验证。源恢复后的 304 可延长新鲜期，200 必须取得完整内容后才替换旧对象。

每次实际回退记录警告，一次共享回源只记一次，不按跟随请求重复记录；不去重、不限频。新鲜缓存命中不记录回退警告，关闭回退或没有可用缓存时不伪造回退事件。这一选择不允许返回部分文件、混用不同应用或来源的数据，不改变发布 Provider 的签名/摘要要求。404/410 仍不属于故障回退。当前版本已经包含回退开关；更细粒度的控制、提示和观测继续保留在 [issue #2](https://github.com/PMExtra/RedApp/issues/2)。

首版不提供身份改名/迁移、Provider 切换、运行时插件、多租户、源认证/私有 CA 新配置、任意查询透传、稀疏分段缓存或通用网站代理。安装器测试与维护沿用 0.6.4 的统一基础，不变更审核过的安装器原文、patch 或信任材料。当前文档及 stacks 配置面向最新运行方式，不承诺旧版本兼容，也不要求用户必须使用某个镜像标签。

### 存储边界与未来后端

新清理/刷新流程以 generation/blob ID、元数据和分页游标工作，不把本地路径放进业务 API。GeneralHttp 的正文读取、Seek/Range、大小查询和删除已收拢到小型存储边界；HTTP 层不依赖 `os.File`。未来对象存储可按范围读取实现所需能力，外部管理接口不必随之更换。

当前仍只有本地文件系统：staging、fsync、rename 发布、启动目录核对及进程锁仍采用本地语义；它们不是远端存储的通用原子性承诺。发布 Provider 的成熟下载/校验流程未为未来 S3 大幅重写，SQLite 仍是本地元数据。S3 客户端、凭据及远端测试均未实现，后续存储契约、multipart 和一致性评估见 [issue #3](https://github.com/PMExtra/RedApp/issues/3)。
