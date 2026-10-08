# RedApp

[English](README.md)

RedApp 通过同一服务分发 **HTTP 文件、Codex CLI 和 Claude Code**。管理员维护厂商与应用资料、选择 Provider 和 BaseUrl，并管理缓存、流量与站点设置。

**0.8.0 为本地候选，尚未发布，使用 SQLite schema 10。** 仅接受新空目录/卷或精确 schema 10。旧 schema 2–9 和未知目录只读拒绝，不迁移、不自动删除；保留原目录可切回对应旧程序。部署文件 schema_version 仍为 1。详见 [配置契约](docs/configuration-v0.8.0.md)及[验收状态](docs/acceptance.md)。

## 快速上手

默认无需配置文件。原生程序执行 `redapp` 或 `redapp serve` 即可；镜像默认执行 `serve`，健康检查使用相同的配置解析规则。

```sh
docker build -t redapp:local .
docker run -d --name redapp --read-only \
  -p 127.0.0.1:8080:8080 \
  -v redapp-v08-data:/var/lib/redapp \
  redapp:local
```

默认监听 `:8080`、数据目录 `/var/lib/redapp`、不信任任何反向代理。RedApp 接受语法合法的请求 Host，域名和网络访问策略由反向代理控制。首次部署以及从 v0.6 升级都必须使用新的空目录/卷；权限失败不会寻找备用目录。

可以只用环境变量启动：

```sh
REDAPP_DATA=/absolute/writable/redapp-data \
REDAPP_LISTEN=127.0.0.1:8080 \
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

容器可选挂载 YAML：在上述命令中增加 `--mount type=bind,src=/absolute/config.yaml,dst=/etc/redapp/config.yaml,readonly`。自定义 YAML/JSON 路径使用挂载加 `REDAPP_CONFIG`。若通过覆盖容器命令传入 `--config` 或其他部署参数，应在 `--health-cmd` 中传入等价参数；Docker 不会将 CMD 参数传给 HEALTHCHECK。全部字段、环境变量、CLI 和限额见[运维说明](docs/operations.md#启动配置)。Provider 与发布信任规则仍是编译期定义；每个应用的 BaseUrl 由后台保存。

打开 **http://localhost:8080/** 浏览应用；管理员入口为 **http://localhost:8080/admin/overview**。初始管理员密码只写入首次启动日志，登录后请修改密码并保护日志。容器以 UID/GID 65532 运行，宿主 bind mount 的权限须提前设置。

自 v0.6.3 起支持在环境变量、CLI 和 YAML/JSON 中使用 `REDAPP_MAX_ARTIFACT_BYTES=4GiB` 等容量简写，兼容原有整数字节值。这是**单个制品文件**的上限，不是缓存总体积：`4GB`/`4gb` 表示 4000000000 字节，`4GiB` 表示 4294967296 字节。默认保持 4 GiB，允许范围为 1 字节至 1 TiB。已发布 v0.6.2 仍需使用整数字节值，见[版本说明](docs/releases.md)。

writer 使用 `REDAPP_MAX_WRITERS`、`download_limits.max_writers`、`--max-writers`，reader 名称不变；三个 Provider 共用这些限额。删除的 Host 与 writer 选项没有兼容别名。

## Provider 与应用管理

在 `/admin/vendors` 添加厂商，再在厂商下添加应用。两者都有全小写 ID、中英文名称/描述和可选 JPG、PNG、静态 SVG 图标。创建后暂不修改 ID、隶属厂商或 Provider；资料、BaseUrl、TTL 和启用状态通过 revision 校验更新。

| Provider | BaseUrl | 缓存与清理 |
| --- | --- | --- |
| 应用介绍 / App Info（`info`） | 无 | 资料和双语 Markdown/HTML/JavaScript 使用说明；无文件路由 |
| 文件托管 / Hosted Files（`hosted`） | 无 | 管理员上传或一次性 URL 导入，持久保存直到手动删除 |
| HTTP Cache（`http-cache`） | 必填；支持 HTTP(S)、端口和企业内网源 | 有序路径 TTL 规则；无匹配规则且无 Cache-Control 时默认 300 秒；按获取或最后访问时间手动或自动清理 |
| Codex（`codex`） | 默认 `https://releases.openai.com/codex`，可覆盖 | 保留发布元数据和制品校验；渠道 TTL 默认 60 秒；按版本清理 |
| Claude Code（`claude-code`） | 默认 `https://downloads.claude.ai/claude-code-releases`，可覆盖 | 保留现有签名和摘要校验；渠道 TTL 默认 60 秒；按版本清理 |

启动时补齐缺失的内置厂商/应用模板，新增条目默认禁用；已有完整键记录和显式空白说明不覆盖。厂商与应用都启用后才公开。完整身份为 `vendor_id/app_id`，`all` 保留给公共目录。匹配内置模板的应用完整键不可删除，即使 Provider 不同；自定义应用可确认永久删除，包括其文件、缓存和历史，仅中止该应用的下载、上传和后台任务，待句柄释放后删除，不影响其他应用且不可撤销。超时会保留待删状态，可重试或重启继续。全局共享上传图标独立保留。换源仍创建独立 source epoch 并保留旧缓存供管理。模板重置默认不选字段，预览差异后按 revision 保存，不删除文件或历史。

首页展示后台排序的置顶项与近七日下载客户端近似去重排行。`/all` 支持可分享的搜索和分页；`/<vendor>` 展示厂商资料与有效启用应用。导航栏提供有上限的异步厂商/应用建议。使用说明以受信任 HTML 文档运行，支持管理员编写的 JavaScript 和外部资源，使用独立文档策略；信任边界见运行说明。

HTTP Cache 将 `/<vendor>/<app>/<relative-path>` 映射到 BaseUrl 下，支持 GET/HEAD、验证器和单段字节 Range，拒绝查询参数和路径穿越。可缓存的冷请求先完整落盘再开始响应，冷 Range 请求也如此；不能共享的响应走有大小限制的直接传输。文件以下载附件返回，不作为可执行网页托管。

HTTP Cache 支持 1–16 个有序镜像 URL，可选择逐个回落、轮询或随机。来源内容、顺序和策略变更会创建新缓存代际；验证器绑定实际来源，跨源重试完整获取。后台可刷新单个缓存资源或 pattern；刷新和清理预览采用服务端分页，执行处理完整冻结集合。

HTTP Cache 设置提供有序缓存规则、默认开启的 `stale_fallback` 开关和可选自动清理规则。匹配对象是解码后、以 `/` 开头的应用相对路径：doublestar glob 匹配文件或目录祖先，Go RE2 正则匹配完整路径，均采用第一条命中的规则。显式规则 TTL 优先于源 Cache-Control，不扣源 Age、不受应用默认 TTL 上限限制；未匹配时，有效 `s-maxage/max-age` 决定新鲜期并扣除 Age/Date。仅完全没有 Cache-Control 时使用应用默认 TTL；存在 Cache-Control 却无有效寿命时取 TTL 0，Expires 不增加另一层优先级。TTL 0 每次先回源，仍可保留完整正文用于故障回退，没有独立 bypass 模式。

显式路径规则可以覆盖源 `no-store/private`，这是管理员主动选择在公开下载入口复用该表示；Set-Cookie、不支持的 Vary 和认证表示隔离边界保持不变。TTL 大于 0 且实际回源覆盖源策略时，每次回源记录一次警告；TTL 0 不产生此覆盖警告。网络失败、超时或上游 5xx 时，`stale_fallback=true` 返回同应用、同 source epoch 下已有的有效完整过期缓存，不设置最大过期年龄；每次实际回退按一次共享回源记一条警告，不去重、不限频，缓存命中不警告。关闭开关则报错，不使用过期缓存。源重验证指令不额外强制验证或禁止回退，但保留 ETag/Last-Modified 条件验证；回退不推进时间戳，404/410 不触发回退。更细控制、提示和观测仍见 [issue #2](https://github.com/PMExtra/RedApp/issues/2)。

手动清理将路径模式、时基和截止时间冻结为预览。自动清理默认规则为空，保存规则后每 15 分钟运行，不在启动时立即删除；仅处理活动 HTTP Cache 应用的当前 source epoch，每应用每轮最多扫描 1000 个文件、退出当前缓存 100 个文件，并通过游标避免后面的文件一直未被扫描。清理按第一条路径命中规则判断年龄，未达到年龄也不继续尝试后面的规则。规则示例、限制和 API 见[运行说明](docs/provider-runtime-v0.7.0.md)。本版不增加源认证、私有 CA 或签名配置；保留基本 TLS 验证以及现有后台和发布校验。

## 配置、维护与交换

可信默认配置嵌入自 `presets/<vendor>.yaml`、`presets/<vendor>/<app>.yaml` 和 `presets/_taxonomy.yaml`。绑定对象保存 template 与稀疏 overrides，独立对象保存完整 spec；等值自定义、显式空字符串/列表仍保持自定义。语言叶独立继承，有序列表及 proxy/prewarm/retention 完整替换。重置取消所选 override，enabled、身份、Provider 和私有 notes 独立。

Codex/Claude 保留当前 source 的最新 N 个完整缓存版本，同时保护有效渠道目标、活动 reader/writer 及无法比较版本，不清理历史 source。发布平台与 HTTP 路径/清单/目录预热复用原校验、授权、下载和缓存链。维护每 15 分钟运行，启动不立即执行。手动预热使用无队列的全局单 worker；默认 10000 文件、深度 16、10 GiB、1 小时，硬上限 100000/32/1 TiB/24 小时。取消仅移除任务的共享等待者，不中断其他公共请求；重启标记 interrupted。双语分类/标签支持公开 category 搜索及最多六项有效关联应用。

后台导出 ZIP，导入 ZIP/单 YAML，可选链接或独立模式。导出所选配置、父 Vendor、引用字典和受控静态图片，排除 Hosted 二进制、运行 UID/状态、缓存/历史/任务；notes 和代理凭据每次默认关闭。预览只展示源码，不执行 HTML/JS；说明变化须明确信任。选择与 UID/revisions/notes 绑定，整包事务提交或回滚。复制创建新 UID/epoch、默认禁用、无旧数据的 App，自身 inherit 在目标 Vendor 下解析。成功回执保留 24 小时，管理员重新登录只读同一已提交结果；未执行预览重启或换会话后失效。[完整字段、限额与 API](docs/configuration-v0.8.0.md)。

## 公共地址与代理

安装链接使用全局公共地址，优先级为：**后台持久化覆盖 > `REDAPP_PUBLIC_URL` 环境默认 > 经验证的请求 origin**。后台清空覆盖会恢复环境默认（若有）；界面显示有效值和来源。只接受无凭据、子路径、查询或 fragment 的 HTTP(S) origin。非空但非法的环境值会阻止启动。

公共地址只影响生成链接；请求同源校验、Cookie 安全属性和上游授权保持独立。企业访问使用 HTTPS 反向代理，将可信代理 CIDR 写入 `trusted_proxies`，并让代理覆盖转发头、控制允许的域名。RedApp 校验 Host 语法并按可信代理链推导请求 origin；来自不可信 peer 的转发头会被忽略。健康检查使用本地监听地址，不依赖公共地址。

站点文案和公共地址为全局设置。回源代理按全局→Vendor→App 解析：`inherit` 使用父级，`direct` 截断继承，`url` 保存完整 HTTP(S)/SOCKS5 URL；Vendor/App proxy 是完整配置叶，父级变化不改写子级 override。全局空 server 表示直连。完整 URL 和百分号编码 userinfo 仅受保护后台 API 可读，公共响应、日志和事件不泄漏凭据。提交经过 revision/CAS 与 transport prepare 后发布，已开始的 reader 使用旧 transport；不执行旧凭据迁移。

## 客户端安装

将 `downloads.example.internal` 换为服务地址。应用详情位于 `/openai/codex` 和 `/anthropic/claude-code`：

```sh
curl -fsSL https://downloads.example.internal/openai/codex/install.sh | sh
curl -fsSL https://downloads.example.internal/anthropic/claude-code/install.sh | bash
```

```powershell
irm 'https://downloads.example.internal/openai/codex/install.ps1' | iex
irm 'https://downloads.example.internal/anthropic/claude-code/install.ps1' | iex
```

命令会下载并执行安装器，请按部署策略先审查脚本。Codex 使用已设置的 `CODEX_RELEASE`，否则选择 `latest`；无人值守 shell 安装时，将 `CODEX_NON_INTERACTIVE=1` 放在管道中 `sh` 前面。说明文档根据公共服务地址生成安装命令。旧公共路径和隐式选择 Codex 的管理 API 不再提供别名。

安装器从本服务开始下载并遵循正常 HTTP 重定向，应用运行期/API 流量不改写。生产上游链路与 Windows/macOS 实机安装仍需上线验证；客户端出口策略独立管理。

## 开发与许可

在 `frontend/` 执行 `npm ci && npm run build` 后，Go 构建会嵌入前端产物。`make check test frontend-test` 覆盖格式/vet/race、离线 Shell/维护及完整 DOM；原生二进制构建一次后，`make runtime-test` 使用临时目录执行 data/HTTP/instructions/retention/prewarm/taxonomy/exchange CLI。官方 Claude 联网与 Windows PS7/5.1 保持独立门禁。

当前变更、Provider ID 和 API 边界见 [0.8.0 配置契约](docs/configuration-v0.8.0.md)。[v0.7.0 说明](docs/provider-runtime-v0.7.0.md)保留作为缓存规则的历史参考。带旧版本号的文档描述对应历史版本，不是当前开发架构的配置指南。

RedApp 原创代码采用 [MIT 许可](LICENSE)。[第三方许可](third_party/README.md)，包括上游安装器 LICENSE/NOTICE，独立保留。
