# 配置

[English](configuration.md)

RedApp 有两类配置：

- **部署配置**控制进程本身：监听地址、数据目录、可信代理和下载限额。它来自文件、环境变量和命令行参数，只在启动时读取一次。
- **运行配置**涵盖其余内容：厂商、应用、站点文案、公共地址和出口代理。管理员在 Web 后台编辑，保存在数据目录中，无需重启即可生效。

## 部署配置

无需配置文件，每个字段都有内置默认值。

### 选择配置文件

RedApp 最多读取一个文件，按以下顺序取第一个：

1. `--config FILE`
2. 环境变量 `REDAPP_CONFIG`（空值视为未设置）
3. `/etc/redapp/config.yaml`

默认文件可以不存在。通过 `--config` 或 `REDAPP_CONFIG` 指定的文件必须存在。所选文件缺失、不可读或不合法时，启动失败。

RedApp 不搜索工作目录，也不会自动查找 `config.json`。

### 文件格式

- 以 `.json` 结尾的文件按 JSON 解析，其他文件按 YAML 解析。
- 文件必须是单个映射，最大 64 KiB。
- 拒绝未知字段、重复键、`null` 值、多个 YAML 文档、锚点、别名、合并键和自定义标签。
- 整数必须是普通数字。只有 `max_artifact_bytes` 额外接受容量字符串。

示例：[config/example.yaml](../../config/example.yaml) 和 [config/example.json](../../config/example.json)。

```yaml
schema_version: 1
listen: ':8080'
data_dir: /var/lib/redapp
trusted_proxies: []
download_limits:
  max_writers: 16
  max_readers: 512
  max_artifact_bytes: '4GiB'
```

### 取值优先级

每个字段取优先级最高的来源：

1. 命令行参数
2. 环境变量
3. 所选文件
4. 内置默认值

列表整体替换低优先级的列表，不合并。每个来源都单独校验，高优先级的值不能掩盖低优先级来源中的非法值。

### 字段参考

| 字段 | 环境变量 | 参数 | 默认值 | 允许值 |
| --- | --- | --- | --- | --- |
| `schema_version` | — | — | `1` | 只能为 `1` |
| `listen` | `REDAPP_LISTEN` | `--listen` | `:8080` | `host:port`；host 为空、`localhost` 或 IP 地址；端口 1–65535 |
| `data_dir` | `REDAPP_DATA` | `--data` | `/var/lib/redapp` | 绝对路径 |
| `trusted_proxies` | `REDAPP_TRUSTED_PROXIES` | `--trusted-proxies` | 空 | 最多 128 个 CIDR；环境变量和参数用逗号分隔 |
| `download_limits.max_writers` | `REDAPP_MAX_WRITERS` | `--max-writers` | `16` | 1–1024 |
| `download_limits.max_readers` | `REDAPP_MAX_READERS` | `--max-readers` | `512` | 1–65536 |
| `download_limits.max_artifact_bytes` | `REDAPP_MAX_ARTIFACT_BYTES` | `--max-artifact-bytes` | 4 GiB | 1 字节到 1 TiB |
| — | `REDAPP_PUBLIC_URL` | — | 空 | HTTP(S) origin；见[公共地址](#公共地址) |

说明：

- `schema_version` 是部署文件格式版本，与数据库 schema 无关。
- 空的 `REDAPP_TRUSTED_PROXIES` 或 `--trusted-proxies` 会清空列表。
- `max_writers` 限制所有应用合计的并发回源数。HTTP 缓存回源和上游 `HEAD` 请求也占用 writer。
- `max_readers` 限制正在服务的并发下载数，包括缓存命中、`HEAD` 和 `304` 响应。
- 任一限额已满时，请求以 `503` 失败，不进入等待队列。
- `max_artifact_bytes` 限制**每个**下载文件的大小，不是缓存总配额。
- `REDAPP_PUBLIC_URL` 不是部署字段，只为公共地址设置提供默认值。非空的非法值会阻止启动。

### 容量写法

`max_artifact_bytes` 接受纯整数（字节）或容量字符串。单位不区分大小写，允许小数。

| 值 | 字节数 |
| --- | --- |
| `4294967296` | 4,294,967,296 |
| `4GiB` | 4,294,967,296（每 GiB 为 2^30） |
| `4GB` 或 `4gb` | 4,000,000,000（每 GB 为 10^9） |
| `1.5GiB` | 1,610,612,736 |

建议使用 `GiB` 等明确的二进制单位以免混淆。不要使用千位分隔符。`max_writers` 和 `max_readers` 不接受单位。

### 校验配置

`redapp config validate` 按与 `serve` 完全相同的方式解析配置并报告错误。它不会打开或创建数据目录。

```sh
redapp config validate
redapp config validate --config ./config.yaml
REDAPP_CONFIG=/etc/redapp/config.json redapp serve
redapp serve --config ./custom.yaml --listen 127.0.0.1:8081
```

`redapp healthcheck` 使用相同的解析规则。如果用参数修改了配置，健康检查也要传入相同参数。见[运维](operations.zh-CN.md#docker)。

## 公共地址

公共地址是生成下载链接和安装命令时使用的 origin，按以下顺序确定：

1. **站点外观**页面（`/admin/settings/site`）中保存的覆盖值
2. `REDAPP_PUBLIC_URL`
3. 当前请求的 origin，由 `Host` 头和可信代理头推导

在后台清空覆盖值后，回退到 `REDAPP_PUBLIC_URL`（如已设置）。设置页显示有效地址及其来源。

规则：

- 只接受 `http://` 和 `https://` origin，允许一个结尾 `/`。
- 拒绝凭据、路径、查询字符串和片段。
- 保存时不会访问该地址。
- 用户已复制的命令不会被改写。

公共地址只影响生成的链接。它不限制 RedApp 接受哪些 `Host`，也不改变 Cookie 安全属性或上游访问。允许的域名请在反向代理配置。

## 可信代理

`trusted_proxies` 列出反向代理的 CIDR，RedApp 只信任这些来源的转发头，其他来源的转发头一律忽略。

- 只填写反向代理所在的地址范围，切勿包含普通客户端的网段。
- 让代理覆盖而不是追加客户端提供的 `Forwarded` 和 `X-Forwarded-*` 头。
- 推导出的请求 origin 决定后台的同源校验，以及会话 Cookie 是否带 `Secure`。

示例见[运维](operations.zh-CN.md#反向代理)。

## 出口代理

RedApp 可以通过 HTTP、HTTPS 或 SOCKS5 代理访问上游。代理按应用分三级解析：**全局 → 厂商 → 应用**。

| 作用域 | 模式 | 设置位置 |
| --- | --- | --- |
| 全局 | `direct`、`url` | **回源代理**页面（`/admin/settings/proxy`） |
| 厂商 | `inherit`、`direct`、`url` | 厂商设置 |
| 应用 | `inherit`、`direct`、`url` | 应用设置 |

- `inherit`（“使用上级设置”）使用上一级的设置，包括上一级为直连的情况。
- `direct` 不使用代理直接连接，并停止继承。
- `url` 使用完整代理 URL，例如 `http://proxy.example.internal:3128` 或 `socks5://user:pass@proxy.example.internal:1080`。

代理 URL 规则：

- scheme 为 `http`、`https` 或 `socks5`，必须显式指定端口。
- 不允许路径、查询字符串和片段。
- 用户名和密码可选，须使用百分号编码。
- 忽略 `HTTP_PROXY` 和 `HTTPS_PROXY` 环境变量。

行为：

- 使用 `http` 或 `https` 代理时，通过 `CONNECT` 访问 HTTPS 上游。使用 SOCKS5 时，由代理解析主机名。
- 修改上级设置不会改写下级自己的设置。
- 修改对新的上游连接生效，进行中的传输在原连接上完成。
- 代理密码永远不会完整返回，后台只显示脱敏值。保存时不修改密码会保留已存储的密码。

## 预置模板与覆盖

RedApp 内置厂商、应用和分类的预置模板，编译在程序中。

- 启动时，缺失的内置厂商和应用以**禁用**状态补齐，不覆盖已有记录。
- 基于预置模板的厂商或应用只保存你修改过的字段（覆盖）。其余字段跟随模板，并在升级 RedApp 时获得模板更新。
- 与模板相同的值、空字符串和空列表也算作你自己的覆盖。
- 英文和中文文本是独立字段。列表（来源、缓存规则、分类、Tag）以及代理、预热和保留设置各自整体替换。
- 内置应用不能删除，可以禁用。

### 重置字段

在编辑器中，只有字段存在覆盖或未保存的修改时，旁边才显示 **重置** 按钮。

1. 点击 **重置**，草稿显示模板值。
2. 点击 **保存**，覆盖被移除，该字段重新跟随模板。

重置不改变启用状态、私有备注、ID 或 Provider，也不会删除缓存文件、托管文件或历史。

### 保存与冲突

每次保存都携带编辑器加载时的 revision。如果期间有其他人保存过，本次保存会被拒绝并保留你的草稿。重新加载查看最新值后再保存。

## 导出、导入与复制

这些功能用于在 RedApp 实例之间迁移配置，或复制一个应用。

### 导出

导出把一个厂商（可选包含其应用）或一个应用生成 ZIP 文件。可选模式：

- **链接**：保留模板引用，只写入你的覆盖。
- **独立**：写入完整的有效配置，不含模板引用。

| 包含 | 不包含 |
| --- | --- |
| 所选厂商和应用的设置 | 托管文件 |
| 导出应用的上级厂商 | 缓存文件、指标历史、任务 |
| 被引用分类的名称 | 内部 ID、revision、source epoch |
| 经过校验的静态图标 | 启用状态 |
| 私有备注，仅在勾选时 | 管理员账号和会话 |
| 代理凭据，仅在勾选时 | 上级代理设置中的凭据 |

两个敏感选项（备注和代理凭据）在每次打开对话框时都默认关闭。如果不导出代理凭据而代理 URL 含有凭据，整个代理设置会被省略并标记为已省略。

### 导入

导入接受 ZIP 文件或单个 YAML 文档。

1. 上传文件。RedApp 显示预览：将创建、更新或跳过哪些对象，以及字段差异。
2. 按需调整选项，例如为单个应用指定目标厂商或新 ID。修改选项会生成新的预览。
3. 如果包内修改了使用说明，需要确认你信任这些说明。说明可以包含 HTML 和 JavaScript；预览只显示源码，从不执行。
4. 执行。整个导入要么全部提交，要么不做任何修改。

规则：

- 新建的厂商和应用为**禁用**状态。被更新的对象保留其启用状态和身份。
- 已有对象默认跳过，除非你选择更新。
- 包中省略的代理设置保留目标对象自己的设置。新对象必须明确选择代理模式。
- 包含未知文件、路径穿越、符号链接、重复路径、发布分发或信任字段的包会被拒绝。

| 限制 | 值 |
| --- | --- |
| 上传大小 | 32 MiB |
| 展开后总大小 | 64 MiB |
| 逻辑实体数 | 1,000 |
| 每个 YAML 文档 | 1 MiB |
| 每张图片 | 2 MiB |

预览 10 分钟后过期，最多保留 8 个。重启或会话变化后预览失效。导入成功后，回执在 24 小时内可读取，重新登录后也可以。再次执行同一导入会返回相同回执，不会重复导入。

### 复制

应用页面上的 **复制** 基于现有应用创建新应用，目标可以是同一厂商或其他厂商。

- 副本为禁用状态，并拥有新的内部身份。
- 不复制缓存文件、托管文件、历史或任务。
- 仅在勾选时复制私有备注。
- 如果副本的代理为 `inherit`，则跟随目标厂商。
- 内置应用的副本可以删除。
