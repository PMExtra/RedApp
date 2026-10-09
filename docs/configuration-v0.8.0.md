# 0.8.0 配置契约

> 本文描述已发布的 0.8.0（schema 10）。其后的 schema 11、多分类、自由 Tag、逐字段重置及移除的关联推荐见 [管理界面与分类标签](admin-taxonomy-v0.8.1.md)，本文“3A”节已被取代。

本文件记录已实现的七个阶段：1A 嵌入预置 YAML、1B schema 10 与稀疏配置 API、1C overlay UI 与三层代理、2A 发布版本保留、2B 主动预热、3A 分类标签、3B 配置交换与复制。当前为 0.8.0 本地候选，未发布。仅接受新空目录或精确 schema 10；旧/未知目录只读拒绝，不迁移、不删除。保留旧目录可使用对应旧程序切回；验收证据见 [验收记录](acceptance.md)。

## 已批准的预置格式

`presets/<vendor>.yaml` 为 Vendor，`presets/<vendor>/<app>.yaml` 为 App。每个文件只含一个实体、一个 YAML 文档，`schema_version: 1`，`kind: Vendor` 或 `App`，`metadata.id` 为 slug；App 另含 `metadata.vendor`。路径、kind、metadata 必须一致；重复身份、缺失厂商、无效 Provider 等导致整包加载失败。

`spec` 为类型化默认配置。Vendor 包含 name、description、icon、localized_icons、proxy；App 包含 name、description、icon、provider、既有 source/cache 字段、instructions 与 proxy。说明采用 `|` 多行标量。App provider 是固定身份属性，后续 overlay 不得覆盖。enabled 不属于预置 spec，首次实体始终禁用；Admin Notes 不属于模板。图标通过相对包路径引用现有审核资源，不接受任意外链或可执行文件。

当前 `entity_templates.json` 的权威数据迁入每实体 YAML；展示/source/TTL 不保留另一份手工 JSON 真值。App 根级 `distribution` 仅由受审查预置加载器接受，保存 protocol、channels、trust_revision、installer_validator、installers/assets 和既有发布策略声明；不能由用户导入或 overlay 修改。签名公钥文本和可执行命令不成为普通 spec。Provider 默认 source/TTL 从预置数据派生。现有五种 Provider 的创建仍可用；内部共用版本层不得直接选择 Provider，不限制创建表单。

预置随构建嵌入，不扫描用户挂载目录或联网更新。Go 为共享解析权威，Python 维护链消费 Go 导出的 JSON inventory；生成文件不是手工权威。每日维护允许改动文件集合、只读 inventory、零 fuzz patch 和信任边界保持。

共享 YAML 解析复用 go.yaml.in/yaml/v3，不新增依赖。拒绝重复键、alias/anchor/merge、自定义 tag、多文档及未知字段；深度最多 32，每实体最多 1 MiB，错误带文件、字段及行信息。保留显式空字符串、false、0、空列表；null 仅在 schema 明确允许时接受，不代表删除 override。部署配置保持其既有大小、空值和类型兼容边界。

### 阶段 1A 接线

运行服务只读取 `presets.Embedded()`；`LoadFS` 仅供可信仓库工具和测试使用。源配置不写 enabled，转换为现有 Store 输入时保持 false。Provider 的默认 source/TTL 按其 canonical 预置身份读取，不由任意同 Provider 的其他预置决定。图标填写相对于 `presets/` 根的 `assets/...` 路径，例如 `assets/builtin/openai.svg`、`assets/openai/codex/icon.svg`。资源树随构建嵌入，加载时枚举并复用媒体校验检查大小、真实格式与静态 SVG；引用必须存在，禁止越界、绝对路径、反斜杠和外链。展示 URL 使用 `/assets/presets/...`，HTTP 只从已校验资源映射返回原始字节及 Content-Type/nosniff。新增展示图标只改 YAML 与资源文件，不修改厂商名称白名单。原协议 `/vendor/app/icon.svg` 使用独立的已审核协议资产注册，不从展示 spec 反推文件。inventory 的展示 icon 变为新 URL；安装器源、validator 和维护文件白名单不变，不要求旧 manifest 展示 icon 逐字相等。

宿主执行 `make installer-inventory`，Go 将 JSON 投影原子写入被忽略的 `.generated/installer-inventory.json`。Python 只读取生成结果，缺失时明确要求先生成，不自动调用 Go。Make 测试/维护入口和 Windows、每日维护 workflow 在进入隔离验证前生成；测试副本复制该文件。隔离容器无需增加 Go 或 YAML 依赖，允许修改文件集合仍仅包含既有安装器原文、generated 和 provenance。历史迁移 JSON 快照已在 1B 移除。

## 共同实现边界

模板引用使用 canonical Vendor ID 或 App vendor/app 字符串。预置 SHA256 对类型化、规范序列化的语义内容计算，不受注释和键顺序影响。模板绑定与实体身份分离，复制到新 ID 可继续引用原模板；内置删除保护只保护系统 canonical 预置实体，不锁定引用它的副本。

导出默认保留 template 与显式 overrides；可选独立副本以 spec 保存当前有效配置。不导出运行 UID、epoch、enabled、Hosted 二进制，默认排除 Admin Notes 和代理凭据。import/复制新建实体默认禁用。空值可覆盖、双语独立、有序列表整体覆盖；enabled、私有 notes 与身份/Provider 不随模板升级变化。

运行持久化仍为 SQLite，不开发旧数据迁移、不改删真实部署数据。HTTP 路径/清单/目录与发布预热、最新 N 缓存版本保留、配置交换/复制、分类标签及关联推荐均已实现，边界见各阶段；所有 Not planned/backlog 排除。

## 阶段 1B：后端/API（未发布）

当前工作分支使用 SQLite schema 10；只接受精确 schema 10 或新的空目录。schema ≤9 和未知目录通过 immutable 只读预检拒绝，要求使用新目录，不迁移、清理或复制原目录。历史升级链及说明默认值迁移 JSON 已移除；旧 DDL 只作为 `internal/store/testdata/` 的拒绝夹具保留。VERSION 尚未统一更新。

`vendor_config` / `application_config` 通过稳定 UID 外键保存权威配置。绑定配置保存 `template_ref` 与嵌套稀疏 `overrides`，不同时保存独立 `spec`；独立配置保存完整 `spec` 和空 overrides。Vendor 引用如 `openai`，App 如 `openai/codex`，与实体当前公开 ID 分开。副本引用模板不获得 canonical 删除保护；当前或历史 canonical 内置身份仍受保护。

`template_snapshots` 保存 kind、canonical key、schema、metadata、最后接受的规范 spec、语义 hash 和当前构建是否存在。hash 使用 schema_version、kind、metadata、规范 spec 的确定性 JSON SHA256；对象键排序，数组保留顺序，说明保留原始字节，URL/HTTP policy 按类型约束规范化。注释、缩进、键排列不影响 hash。distribution 不参与普通 spec/hash/overlay。

启动先校验所有预置和候选有效配置，再以同一事务更新 snapshot、配置和有效投影。模板 hash 变化令绑定实体 revision 增加一次，包含被自定义字段遮住的默认变化。缺失模板保留最后 snapshot 与有效配置，标记 missing，不删除/禁用或清缓存；未知引用不能首次创建绑定。Provider 和模板 metadata 身份契约不能在重现时改变。被移除图片返回现有资源失败结果，不联网补图。

`trusted_distribution_snapshots` 独立保存最后接受的受审查分发 descriptor 与 digest，只能由可信嵌入构建的内部协调器写入。模板缺失时保留该快照；启动与每次发布仍验证当前注册的相同协议、固定 trust revision/验证器及必需安装器、静态资源。不恢复已移除的协议代码、不从快照加载签名密钥或执行命令；验证失败则在写事务前拒绝，数据库不部分更新。该内部快照不进入普通配置 API、spec、override、复制或导入导出。分发摘要与语义 hash 分开，变化推进关联实体 revision 一次（同时 spec 变化也只推进一次），不产生用户 override。

### configuration API

需通过现有登录、Origin、CSRF 边界：

- `GET/PATCH /admin/api/vendors/<vendor>/configuration`
- `GET/PATCH /admin/api/apps/<vendor>/<app>/configuration`

GET 返回 `instructions_revision`（有说明时）、管理专用 `proxy_effective`、`revision`、`template_ref`、`template_hash`、`template_missing`、`defaults`、`overrides`、规范化 `effective` 和 `fields`。每个字段含 `source: inherited/custom` 与 `differs_from_template`。独立实体 defaults/hash/ref 为 null，字段来源 custom，差异为 null。GET 不产生覆盖、revision 或配置写入。公开 DTO 只使用有效值投影，不返回上述完整对象或私有 notes。

PATCH 示例（revision 必须为当前正整数；如另带 `If-Match: "7"`，必须与 body 一致）：

```json
{
  "revision": 7,
  "set": {
    "name.en": "Enterprise Codex",
    "description.zh-CN": "",
    "instructions.en": "",
    "cache_ttl_seconds": 120
  },
  "unset": ["description.en"]
}
```

持久化后的 overrides 使用嵌套对象，而不是路径字符串：

```json
{
  "name": {"en": "Enterprise Codex"},
  "description": {"zh-CN": ""},
  "instructions": {"en": ""},
  "cache_ttl_seconds": 120
}
```

允许 name/description 的 en、zh-CN，icon，以及 Vendor/App 的整体 proxy；Vendor 另有 localized_icons 两语言；App 另有 instructions 两语言和 source/cache 字段。HTTP Cache policy 为 `http_policy.rules`、`http_policy.auto_cleanup`、`http_policy.stale_fallback` 三个字段，列表整体替换，不合并规则内部。实际仍执行 Provider、来源、policy、图片和文本约束。HTTP Cache 的有序来源通过 base_urls 编辑；base_url 是第一来源的规范投影，单独改变它须使用 base_urls；旧目录 base_url 单来源请求由适配层生成相应列表。

缺字段继承；空串、空列表、false、0 不代表 unset，合法性依字段而定（例如规则列表可以为空，HTTP 来源列表必须非空）。null、未知路径、provider/id/vendor/UID/enabled/notes/distribution/trust，以及 set/unset 的冲突均拒绝。与默认值相同仍为 custom。无模板的实体不能 unset；空操作 PATCH 不推进 revision。

### 旧入口与 revision

目录 PATCH 只把请求明确提供的语言叶字段或配置字段设为 override；enabled 使用单独开关。说明 PUT、HTTP policy PUT、TTL 写入及 template reset 汇入相同权威路径。reset 组映射到 unset，不写默认值，也不恢复 enabled/notes。模板 GET/reset 依据存储绑定，而不是副本的公开 key。说明 PUT 保留说明自己的 CAS，返回最新 `entity_revision`；entity revision 与配置/有效说明同事务更新。

| 操作 | 配置 revision | runtime_revision | Instructions revision | Source epoch |
| --- | --- | --- | --- | --- |
| 空操作 PATCH、GET、注释变化、同内容重启 | 不变 | 不变 | 不变 | 不变 |
| 资料、图标、说明、字段模板/自定义状态 | 增加一次 | 不变 | 仅相关语言有效文本或 inherited/custom 状态改变时增加 | 不变 |
| enabled 或删除/tombstone | 增加一次 | 增加一次 | 不变 | 不变 |
| 有效 source URL、列表/顺序、策略变化 | 增加一次 | 增加一次 | 不变 | 增加一次 |
| 有效 TTL、HTTP policy 变化 | 增加一次 | 增加一次 | 不变 | 不变 |
| 已绑定模板语义 hash 变化 | 增加一次，即使有效字段被遮住 | 仅有效运行字段变化时增加 | 按文本/状态变化 | 仅实际 source 变化 |
| 可信分发摘要变化 | 增加一次 | 增加一次 | 不变 | 不变 |
| App/Vendor proxy 或 Global proxy | 本层增加一次；子级不变 | 不变 | 不变 | 不变 |
| 模板 missing 标记变化、冻结有效配置 | 不变 | 不变 | 不变 | 不变 |

`vendors/applications.runtime_revision` 初始为 1，由 `materialize` 的一个分类器统一推进。Entry 同时带配置与运行版本。SourceFence、generation、清理/发布围栏中的既有 AppRevision/VendorRevision 字段改为运行版本语义；纯资料或代理更新不会 retire 已准入 writer。Notes 继续独立 CAS。实际策略变化仍拒绝旧围栏发布。

创建独立空说明不假设存在继承来源；空文本再次保存不为仅相同内容推进说明 revision。内置预置初次补齐禁用，既有同名独立实体不自动绑定；重启不重启用。

### 事务与内存发布

API 使用 directoryMu；Store 统一配置协调器串行包括直接调用者。候选从同一只读快照构造；写事务前本地构造并验证完整 Registry、source clients 和 Downloads 注册计划，不执行网络。事务中重新核对全部参与实体 revision/epoch、配置与模板 hash，失败撤销候选。

锁序：`publication gate → Downloads.mu 短暂 prepare → 释放 mu → DB CAS 事务 → commit/释放连接 → Downloads.mu 内存发布 → 已验证 Registry 指针交换 → 释放 gate`。普通下载、缓存命中和网络不取得 gate；DB 事务内绝不取得 Downloads.mu。Close 先取得 gate 才设置 closed。提交后的 publish 不解析 URL、不读 DB、不返回业务错误。极短 DB 新 fence / registry 旧快照窗口允许旧请求被 fence 拒绝，不声称跨 SQLite/内存 ACID。

删除先通过同一协调器发布禁用 tombstone 和持久删除意图，再取消/排空工作；已被隔离的物理清理可复用现有 Downloads.mu 回调，不重入发布门。此阶段未增加全局传输锁或任务总线。

1B 的后端/API 已在 1C 接入字段继承与草稿表单及三层代理。预热/清理扩展、导入导出、分类和大幅 UI 重设计仍暂停。


## 阶段 1C：overlay UI 与三层代理（未发布）

Vendor/App spec 的 `proxy` 是一个整体叶字段，预置省略时规范为 `{mode:"inherit"}`。有效格式仅为 `{mode:"inherit"}`、`{mode:"direct"}` 或 `{mode:"url",url:"完整 URL"}`；后两类 URL 使用既有 HTTP/HTTPS/SOCKS5 校验、端口与凭据长度约束。非 url 模式不接受 url 键。Global 仅 direct/url；旧空 server 兼容 direct，新 API/UI 使用明确 mode。`unset proxy` 恢复模板字段，与显式 set `{mode:"inherit"}` 跟随父级网络设置分别处理。

运行解析 App → Vendor → Global → direct。管理 configuration GET 单独返回 `proxy_effective:{mode,url,source_scope,source_id,dns}`，source_id 是 canonical 公开身份；传输选择使用稳定 App UID 与 Vendor UID。保存完整代理 URL 只对管理员开放，不进入 public DTO/bootstrap/search、events、错误或日志。父级修改不固化子级继承、不推进子级配置/运行版本或 epoch。

Pool prepare 本地构造不可变 scope→transport 快照，同一有效 URL 可复用既有 public/configured 两种 transport。配置 coordinator 的锁序为 `directoryMu → configMu → Pool.proxyMu → Downloads.publicationMu → 短暂 Downloads.mu`；DB 事务期间不取 Downloads.mu，commit 后发布整张 scope 快照、注册新 source clients 并发布 Registry。Global proxy 使用相同 coordinator/CAS。失败 Abort 不改 DB/transport。更新只关闭被替换 transport 的 idle 连接；正在返回的 body 继续使用原连接，下一次 RoundTrip 使用新选择。

当前及历史 source clients、catalog、download、HTTP cache 多源与 Hosted URL import 均携带应用作用域。未知/错误厂商/删除作用域拒绝新增网络，不回落 global；Info 和 Hosted 本地文件无网络。静态编译协议夹具仍保留无动态目录的 legacy client 构造器，生产动态服务使用 scoped 构造器。`SeedDirectory` 明确只在安装 publication coordinator 之前初始化；生产不调用它，运行时调用拒绝。

DirectoryEditor、双语说明、release TTL 与 HTTP policy 使用 configuration 的 effective baseline、override 状态和显式 dirty set/unset。无变化保存不写；等值自定义仍显式 set，不按值相等 unset。语言字段分别处理，列表整体处理。紧凑字段状态可对比默认/当前值、设为自定义或恢复模板；独立实体无虚假恢复，missing 模板保留提示与最后接受默认值。分组 reset 预览后映射为 unset，enabled/notes 不参与。

保留草稿离开保护、对象切换取消旧请求、409 输入保留及最新配置 revision；即时 enabled 开关只写自身并合并新 revision。configuration 响应带说明 revision，旧说明 CAS 入口仍经同一权威配置事务。

## 阶段 2A：已缓存发布版本保留（未发布）

仅 Codex/Claude App 接受原子叶字段 `retention: {enabled: false, keep_latest: 3}`。N 为整数 1..1000，默认禁用；修改只推进配置 revision，不推进 runtime_revision/source epoch。模板、自定义和 unset 恢复仍使用 configuration API。

共用 `internal/releasemaintenance` 只选择当前 source 中至少含一个 current、非 retired、complete 二进制 generation 的版本，按 Provider 版本比较降序保留 N 个，不按时间，不计 metadata-only。再并集声明渠道当前经验证且 TTL 有效的指向、任何 reader/running writer 版本及无法可靠比较版本；实际可多于 N。渠道验证失败整轮跳过，不自动下载二进制。历史 source 不清理。

`POST /admin/api/apps/<vendor>/<app>/retention/preview` 带配置 revision，冻结现有精确 generations、策略语义 hash、runtime fence、渠道指向/取回时间/到期及每版理由。`GET .../retention/<id>/items?page=1&limit=25` 分页查看；`POST .../retention/<id>/execute` 显式确认，`GET .../retention/status` 返回每应用仅一条持久最近结果。预览 10 分钟，执行回执 24 小时，复用既有 cleanup_previews 修剪，不建立队列或无限历史。

自动安全执行持有现有 Downloads.mu，在 durable retirement 前检查 m.all 中使用状态并按整版排除；同一 Store 事务复查当前 active source/runtime、retention hash、渠道指向与新鲜度，记录最终 generation 子集和 skip 理由。后出现的 generation 不扩入冻结集合；空结果成功。原手动最低版本清理仍可退役在用 generation 并排空，语义不变。配置、metadata 和下载统计均保留；回收复用原 generation/blob 引用计数，逻辑退休字节不等于立即释放的共享 blob 字节。

生产调度单串行 worker，每 15 分钟一次，启动后等待首个周期，每应用每轮最多退役 100 个版本，后续周期继续。禁用/换源/策略变更使旧预览无法执行；关闭/删除沿用 context 和 ApplicationWork。管理页提供启用删除确认、N、overlay 控件、保护说明、最近结果和分页预览确认。2B 主动预热使用以下独立 worker；不引入通用队列。


## 阶段 2B：主动预热（未发布）

仅 Codex/Claude 提供可选的 PlatformProtocol，不扩大通用 Protocol。Codex 六个平台固定 Linux musl，优先选择 codex-package 目标 tar.gz 和共享 SHA256SUMS，缺失时回退同版本 npm tgz；Claude 八个平台选择已验签 manifest 中的平台 binary。所有资源仍经 Catalog.Release → Authorize → Downloads.Acquire，读取结束后等待最终大小/摘要验证；完整命中直接关闭 reader，不重复读取文件。发布 App 原子 `prewarm: {enabled:false, channels:[], platforms:[]}` 仅改变配置 revision。启用须声明合法频道及平台；自动检查共用现有 15 分钟循环，启动不立即执行，已成功的版本/资源/平台/策略指纹跳过，失败下一轮重试。

HTTP 手动输入仅接受应用相对路径、UTF-8 路径清单、目录根及可选既有 glob/RE2。筛选器只过滤发现的文件；无索引时仅匹配已有缓存路径。索引支持 HTML a[href]、nginx autoindex JSON、Caddy browse JSON，HTML 使用固定 x/net/html。每根索引选择一个镜像后整棵遍历固定该镜像；链接及重定向限制在同 origin/BaseURL 根内，拒绝凭据、query/fragment、点段、编码分隔符和双重解码，不合并不同镜像目录。目录由类型、is_dir 或尾斜杠辨认；父级、排序、重复及越界链接用有限原因计数忽略。索引读取上限 2 MiB；未出现在列表中的已有缓存不删除。

已有 HTTP 文件先对所选来源 HEAD，使用该来源验证器。合法 304 或同来源非空 ETag 且长度相同只更新验证时间；同来源变化 ETag、Last-Modified 或长度强制 GET，即使 TTL 未过期；同来源相同非空 ETag 且长度一致优先于冲突的 Last-Modified。另一镜像的验证器/长度视为未知，TTL 尚有效时不据此判变、不发 GET、不触碰 validated_at/fresh_until；TTL 过期才从所选镜像 GET。405/501 回退条件 GET。无法比较验证器才允许 TTL 回退，保留验证时间；HEAD 网络/5xx 是失败或旧缓存回退，不计成功。GET 使用初选镜像再逐镜像验证，不跨来源复用验证器，沿用 generation fence、策略、容量和 sharedFetch。预热不伪造公共请求，不更新 last_access 或公共 miss 计数；实际回源字节仍记账。不可缓存响应立即关闭 body。

专用全局 worker 同时只运行一个任务，无排队；忙时 409 返回当前任务摘要。`POST .../prewarm/start`、`GET .../prewarm/<id>`、`GET .../prewarm/<id>/items?page=1&limit=25`、`POST .../<id>/cancel`、`POST .../<id>/retry` 均在原管理认证/CSRF 路径下，并按 App UID 校验所有权。请求 ID 幂等，重试创建新任务、冻结已解析发布版本并排除先前成功项。输入及 source/runtime fence 持久化，状态响应不包含输入路径清单或上游 URL；条目默认 25、最多 100。终态保留 24 小时并有限修剪。关闭和重启标记 interrupted，不恢复执行；取消仅撤销本任务的共享等待者，公共请求仍可继续共享回源。删除沿用 ApplicationWork 取消与排空。

默认限额文件 10000、深度 16、任务读取 10 GiB、时长 3600 秒；硬上限分别 100000/32/1 TiB/86400 秒。索引、文件及重试实际读取计入任务；已知长度可提前拒绝，HEAD 头不计字节。**任务读取上限不是服务总网络上限**：任务取消或达到限额后，已有公共等待者仍可继续共享网络。达到限额终态 limited，不伪报完成成功。管理页提供两种输入、平台选择、UTF-8 上传、高级限额折叠、进度分页、取消/重试及发布自动策略 overlay/unset。

目录格式依据：[nginx 官方模块说明](https://nginx.org/en/docs/http/ngx_http_autoindex_module.html)、[nginx 1.28 源码](https://github.com/nginx/nginx/blob/release-1.28.0/src/http/modules/ngx_http_autoindex_module.c)、[Caddy 2.10.2 browse](https://github.com/caddyserver/caddy/blob/v2.10.2/modules/caddyhttp/fileserver/browse.go) 与 [fileInfo 字段](https://github.com/caddyserver/caddy/blob/v2.10.2/modules/caddyhttp/fileserver/browsetplcontext.go)。测试保留这些实际字段形状及越界/循环/限额用例。

CLI 验证：`python3 scripts/test-prewarm-cli.py` 使用本地真实格式 fixture，验证冷目录、可信 Codex 发布平台、缓存命中及重启。`python3 scripts/test-prewarm-claude-cli.py` 是显式联网集成检查，需要网络出口允许 `downloads.claude.ai`，使用官方签名 fixture 对应的一个真实二进制，不执行它，临时目录退出时清理。当前云环境对该域名 CONNECT 返回 403，此成功下载检查暂被网络条件阻断；官方签名/篡改拒绝与八平台映射的本地测试已通过。

Claude 完整组件成功链另以 `TestSignedClaudePrewarmRealComponentPipeline` 验证。测试运行时创建 RSA4096/SHA512 密钥和小型签名 manifest，仅从同包 `_test.go` 设置未导出的验证函数，并调用共享 pinned-key 校验逻辑；生产构造器固定官方 Verify，无配置/环境变量/CLI flag/导出 API 换信任根，默认二进制不含测试公钥。覆盖签名拒绝、二进制摘要失败、metadata-only、完成及零读取缓存命中。真实官方联网 CLI 仍为 network blocked，不计通过。

## 3A：分类、标签与公开关联

App 的 `category` 是可留空的 slug，`tags` 是稳定按 ID 排序且拒绝重复的 slug 数组；空字符串与空数组都是显式叶覆盖，分别支持 unset。配置和公开目录修订增加，runtime_revision/source_epoch 不变。字典改名仅更新字典 CAS 与公开修订，不改 App 修订，不重建运行对象。

唯一可选根级字典是 `presets/_taxonomy.yaml`，`schema_version: 1`、`kind: Taxonomy`，spec.categories/spec.tags 各为 `{id,name:{en,zh-CN}}` 列表。空字典和未分类默认不虚构条目。可信 YAML 加载器聚合后检查所有引用，拒绝未知 ID、重复标签、空语言名称与非保留路径的 Taxonomy。字典每语言保留默认值和显式覆盖；同值覆盖也保留，未覆盖语言随默认升级，失去预设的内置字典保留最后名字及引用，不允许 UI 删除。新增预设遇到管理员自建同 ID 条目时保留其自建所有权和名称，不自动绑定继承/恢复；有效预设引用仍参与删除保护。

schema 10（尚未发布）的 taxonomy、application_categories、application_tags、template_taxonomy_refs 表提供具体关联索引；不另加迁移或公开 JSON 扫描。App 有效字段投影与 configuration 同事务提交。删除用户条目检查所有未删除 App（包括禁用）及有效预设默认引用，409 返回 references 数量；不隐式清空引用。

认证/CSRF 后提供 GET `/admin/api/taxonomy?kind=categories|tags&q=...&page=...&limit=...`、POST `/admin/api/taxonomy/{kind}`、PATCH/DELETE `/admin/api/taxonomy/{kind}/{id}`。PATCH 仅接受 name.en/name.zh-CN set/unset 与 revision CAS，ID 不可改。管理入口仅在应用管理工具栏；独立页双 Tabs、现有分页/删除确认/字段覆盖恢复，冲突保留草稿，由显式 Reload 决定重新载入。

`/all?category=tools&q=...&page=...` 服务端先过滤可用实体再分页；分类选项来自可公开 App，不带禁用计数。公开 DTO 只投影 category/tags 的 ID 与双语名称。相关应用先按共享标签数，再按已有七天客户端估计，再按 canonical key 排序，最多六项；排除自己及禁用/删除 App/Vendor，至少一共同标签，无结果不显示版块。候选与热门分数各批量读取一次，不下载或回源统计。

3A 不增加自动推荐标签/分类、树结构、颜色或权限模型；配置导入导出由下一节 3B 提供，当前候选未推送、发布或部署。

## 3B：配置 ZIP / YAML 交换与 App 复制

普通交换文档与可信预设使用不同类型。根级仅接受 schema_version、kind、metadata、spec，或 template/template_hash/overrides，以及可选 admin_notes、omitted_fields。schema_version 固定为 1；kind 为 Vendor/App/Taxonomy。Vendor 的 metadata.id 与 App 的 metadata.vendor/id 是公开身份。任何 distribution、信任、公钥或可执行声明在根级及 spec/overrides 中都拒绝；不导入 UID、revision、epoch、enabled、全局账号/session/secrets、缓存、统计、任务或 Hosted 二进制。

独立文档的 spec 包含完整当前有效配置。链接文档仅输出 canonical template、语义 template_hash 和嵌套稀疏 overrides；不能同时有 spec，不输出整份 defaults/effective。等值自定义、空字符串、空数组和语言叶覆盖原样保留。template_hash 用于提示目标模板不同，不锁定旧版本。无绑定的实体以独立文档导出。独立模式只解除模板关系，proxy.mode=inherit 仍按目标父级解析，不复制父级 effective proxy 凭据。

POST `/admin/api/configuration/export` 接受 selection（kind/key；Vendor 可指定 include_apps）、mode=linked|independent、include_notes、include_proxy_credentials。selection 必須非空；Vendor 默认含 Apps，App 默认附带父 Vendor 的独立文档。响应为附件 ZIP：`presets/<vendor>.yaml`、`presets/<vendor>/<app>.yaml`，被引用字典有效名称放在 `presets/_taxonomy.yaml`，受控静态图片为 `presets/assets/<sha256>.<真实扩展名>`，文档以 assets/... 引用。路径与内容顺序固定，多行说明使用 literal scalar，同内容图片去重。只读取受控媒体或已验证的嵌入资源，不联网获取图标。导入静态图片经过完整静态校验后保留原始字节，避免重复 JPEG 编码和内容 hash 漂移；原图标上传接口的标准化行为不变。

两个敏感选项每次打开都默认关闭。notes 仅在显式选择时写入根级 admin_notes 纯文本。对象自身代理 URL 有 userinfo 且不允许导出凭据时，省略整个 proxy 字段，并标记 omitted_fields: [proxy]，不生成假密码、直连或继承值。

导入预览 POST `/admin/api/configuration/import/preview` 为 multipart：一个 file（ZIP 或单文档 YAML）、可选 choices JSON 数组。单文档身份来自 metadata，不使用上传 basename。ZIP 先验证目录和尺寸，再限量读取；拒绝未知文件、重复路径、symlink、../、绝对路径、反斜杠、百分号编码逃逸、路径与 kind/metadata 不一致、多文档、alias/anchor、未知字段及未知分类/标签引用。上限为上传文件 32 MiB、总展开 64 MiB、1000 个逻辑实体（Vendor/App/字典条目），每 YAML 1 MiB、每图 2 MiB，并沿用 4096 维度 / 4M 像素与静态 SVG 限制。失败整体拒绝；不执行任何包内文件。预览在内存最多保留八个，十分钟过期，临时图像验证目录立即清除。

新实体默认 create；已有实体默认 skip。预览列出目标、当前 UID/revision、notes revision、模板差异/missing、拟议字段源码差异、字典名称冲突与遗漏字段。单 App 可选择已有目标 Vendor 与新 ID；其他文件沿用身份。目标 Vendor 必须已存在或在包内明确创建，不补默认空厂商。已有字典默认保留名称；显式 dictionary_update 对双语名称做 CAS，内置所有权不变，以逐语言 override 保存。无当前模板的新链接拒绝，需使用独立副本；已绑定同一缺失模板的已有对象在显式 update 时可使用最后接受快照。Provider 不可改。独立 spec 更新已有 linked 对象须明确 detach_template；含 notes 的 update 须明确 update_notes，并绑定 notes revision。

omitted_fields: [proxy] 更新默认保留目标自身代理配置与原有覆盖状态；新对象必须明确选择 inherit/direct/URL。若模板解绑或改绑定原本会改变有效代理，必须显式选择 keep_effective_proxy 或新 proxy，不替用户选择出口。保留当前有效设置会成为自身配置/override。普通导入没有 notes 时保留目标 notes。说明差异仅以源码文本展示，不渲染 HTML/iframe，不执行脚本或访问外链；有实际说明变更时执行须 trust_instructions 明确信任其 HTML/JS，现有说明文档渲染语义不变。

choices 按 kind/key 标识，可提供 action、单 App target_vendor/target_id、dictionary_update、proxy 或 keep_effective_proxy、detach_template、update_notes。改变选择后重新生成预览，执行不能修改选择或上传新内容。预览 ID 随机不可猜，绑定内容摘要、当前会话及配置快照（UID/revisions、模板 hash、字典与 notes revision、代理上下文）。POST `/admin/api/configuration/import/<id>/execute` 仅接受 confirm 和 trust_instructions。重新检查快照和提交时会话；同 ID 删除再建也因 UID 不同而冲突。新对象始终 disabled，update 保留启用状态与 UID/运行身份；源实际变化才按已有 epoch 规则隔离。

整包配置、字典和选定 notes 通过现有 prepare→CAS→事务提交→publish 一次应用。图片先在隔离目录验证；执行仅落盘最终配置引用的图片，媒体上传与导入发布/回滚串行，失败只清除本次新建且仍无引用的文件，不删除既有共享图标。短回执与配置同事务保存，重复执行幂等返回；超过 24 小时的回执在后续执行时清理。重启使未执行预览失效，不自动执行。成功回执仅保存操作 ID、目标 UID 与结果摘要，不保存可重执行 payload、notes 或凭据。当前有效管理员重新登录后，可在正常认证/CSRF 后使用旧 execute token 读取 24 小时内的持久成功结果，绝不重新导入；没有成功回执或已过期的 token 无此例外。未执行预览仍绑定原会话和进程，重启或换会话后必须重新预览。

POST `/admin/api/apps/<vendor>/<app>/copy` 接受 source_uid/source_revision、target_vendor/target_id、mode linked|independent，以及可选 include_notes/notes_revision。linked 原样复制绑定及稀疏 overrides；无绑定只能 independent；独立复制当前有效 spec。保留自身完整代理设置，inherit 在目标 Vendor 下重新解析；不通过导出中转敏感信息。notes 默认不复制，勾选时单独 CAS。新 UID、epoch 1、disabled，无旧缓存/统计/任务/Hosted 文件；完整 key 冲突为 409，不自动覆盖或变 ID。内置 App 的副本仍可删除。所有配置、说明、图标引用和分类/标签关联同事务协调，成功跳转新 App 管理页。

全部接口保持管理员 session/Origin/CSRF。预览包和敏感选项不写入 localStorage，不出 public/log；错误不回显凭据或说明正文。界面复用现有 Lucide、弹层、选择/上传/分页与表单控件，工具栏提供导入，Vendor/App 详情提供导出，App 另提供复制。本地候选 VERSION 为 0.8.0，未发布。
