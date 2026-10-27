# JSON 发布分发

`json-release` Provider 从 JSON API 发现经过校验的安装包。应用配置请求与 jq 表达式；新增使用相同协议的应用不需要再增加一个 Provider。

## 配置与预览

创建 JSON 发布应用并提供 `json_release` 对象，或使用 Visual Studio Code、IntelliJ IDEA、PyCharm、CLion 内置模板。在应用设置中编辑配置。预览获取元数据和校验侧车文件，展示规范化安装包，并允许选择精确预热 key。保存沿用 revision 检查；仅预览不会发布快照或缓存安装包。

每个渠道定义 `requests`（URL 与字符串 `context` 的显式矩阵）和 `query`。每次请求单独向查询提供 `{body, context}`，必须返回一个对象：

```json
{
  "version": "1.2.3",
  "build": "upstream-build-id",
  "artifacts": [{
    "key": "windows-x64-user-exe",
    "os": "windows",
    "arch": "x64",
    "format": "exe",
    "scope": "user",
    "url": "https://downloads.example/app/setup.exe",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "size": 123
  }]
}
```

这只是示例数据，不是真实摘要。`build`、`scope`、`variant`、`size` 可省略。矩阵所有结果的 version/build 必须一致，拒绝拼接平台版本不一致的发布。key 必须唯一且跨版本稳定，不同包型或安装范围须使用不同 key。

系统值为 `windows`、`macos`、`linux`、`unknown`；架构为 `x64`、`arm64`、`x86`、`arm`、`universal`、`unknown`。在表达式中映射厂商别名和跨层字段，不猜测未知值。`format`、`variant` 接受有长度限制的小写标识符，新增包型不需要新增枚举。

也可提供 `checksum_url`、`checksum_filename`。引擎接受单个 SHA256，或按精确文件名匹配的 SHA256 清单行；冲突或歧义会失败。摘要与侧车同时提供时必须一致。没有预期 SHA256 的安装包不会获得下载授权。

## 来源与资源限制

`sources.metadata`、`sources.artifacts`、`sources.checksums` 分别配置 `{origin, prefix}`。origin 是不含凭据或查询参数的 HTTP(S) 来源，prefix 是 `/products/` 一类目录。初始 URL 和每次重定向均按角色单独授权；元数据不会自动授权响应中任意 URL。保留现有代理作用域、TLS 和解析地址限制。HTTPS 不允许降级到 HTTP，跨来源重定向必须使用 HTTPS；配置代理时目标 DNS 策略由代理负责。

表达式属于可信管理员配置，不安装环境变量、外部模块或自定义网络/文件/进程能力。限制包括每渠道 32 次请求、128 个制品、元数据和输出各累计 4 MiB、查询文本 16 KiB、单个侧车 64 KiB（累计 1 MiB）、发现过程 30 秒、查询 context 2 秒和最多两个并行查询。取消与输入/输出限额**不能硬限制中间内存，也不能抢占所有操作**。预览不是沙箱；严格隔离跟踪于 [issue #18](https://github.com/PMExtra/RedApp/issues/18)。

## 刷新、预热与下载链接

`refresh_seconds` 为 60–86400 秒；每分钟调度一次到期渠道，独立于版本保留。刷新失败记录失败状态并保留旧有效快照。发布元数据、授权资源和渠道绑定在同一事务中提交。来源、查询或渠道变化切换来源代次；仅修改刷新间隔或预热选择保留缓存制品。

`prewarm` 为 `{channel, keys}` 数组。元数据发布后复用现有有容量限制的预热 worker，空选择关闭自动预热。worker 忙或下载失败不会使元数据失效，后续刷新可以重试。预热绑定本次快照的固定版本，不回退到其他架构或旧版本。

- `/{vendor}/{app}/download/latest/{key}` 解析配置的默认渠道。
- `/{vendor}/{app}/download/stable/{key}` 解析显式 stable 渠道。
- 两者均返回不可缓存的临时重定向，目标为 RedApp 内部 `/{vendor}/{app}/releases/{release_id}/{key}`。
- `/api/apps/{vendor}/{app}/downloads?channel=latest` 从同一快照返回规范选项与本地 URL。

公开选择器尽力依据浏览器信息推荐。架构不可靠或有多个包型时要求手选，不猜测 Linux 包管理器；手动选择优先。

不透明 release ID 标识 version/build，不能按语义版本排序。此 Provider 首版不启用旧安装器专用的自动版本保留策略，不能通过排序摘要 ID 删除版本。

## 预置与更新策略

VS Code 使用各平台更新 JSON：`productVersion` 为显示版本，`version` 为上游 build/commit，`sha256hash` 为摘要。JetBrains 使用产品发布 JSON，映射 `downloads` 中支持的平台，排除库元数据。预置配置为 IDEA 的 `IIU`、PyCharm 的 `PCP` 和 CLion 的 `CL`（安装包与校验文件限定在 `/cpp/`）。这些端点选择仍受下述验证边界限制；其他产品代码可以复用配置。

模板具有映射 fixture，其中 dotUltimate 使用记录的官方响应及校验文件。无需下载安装包即可验证官方元数据和小型 sidecar，但这不能验证安装包字节或安装行为。启用预热前请在实际网络中执行预览。CDN 或校验格式改变会拒绝发布，需明确调整配置。

分发原始 IDE 安装包不会修改安装后的设置、启动器或官方更新器。这些模板不会禁用 IDE 更新检查，也不会将其指向 RedApp。目前未验证适用于这些原始安装包流程的稳定安装期适配方式；管理员必须了解这一边界，不能认为下载代理已经接管运行时更新。

### 候选预置清单

12 个 JSON Release 预置为 VS Code、IntelliJ IDEA、PyCharm、CLion、WebStorm、PhpStorm、GoLand、Rider、DataGrip、RubyMine、RustRover、dotUltimate。新预置默认禁用且不预热，官方元数据及校验文件已经核对，**完整安装包下载与安装仍未验证**。不包含 MPS、DataSpell、AppCode、Fleet、Aqua、Writerside、Gateway、Toolbox。

| JetBrains 候选 | 配置产品码 | 制品/校验文件前缀 |
| --- | --- | --- |
| IDEA / PyCharm / CLion | IIU / PCP / CL | /idea/ /python/ /cpp/ |
| WebStorm / PhpStorm / GoLand | WS / PS / GO | /webstorm/ /webide/ /go/ |
| Rider / DataGrip / RubyMine | RD / DG / RM | /rider/ /datagrip/ /ruby/ |
| RustRover / dotUltimate | RR / RSU | /rustrover/ /resharper/ |

启用前仍须通过预览验证当前元数据、平台可用性、CDN 重定向与 sidecar。记录的响应和 CI fixture 不能替代完整下载与安装验证。

dotUltimate 是与独立 Rider 并存的 Windows .NET 套装。官方 `RSU` 元数据通过 `downloads.windowsZIP` 标识完整离线归档。预置要求该条目及 ZIP URL；缺少 ZIP 元数据、在线安装器（`windowsWeb`、`windowsWebLP`）及独立的 `toolboxWindows` 包均不会被替代选用。离线 ZIP 整体作为一个制品，完整保留 EXE 和 .dat，不解包、不为每个子组件重复登记。未知架构保持未知。元数据与 SHA256 sidecar 不能证明归档内容或离线安装行为；启用预热前仍须验证。

## 存储与部署

来源快照和刷新状态使用 schema 16。schema 15 通过迁移前数据库备份及校验事务原地升级；其他不支持的 schema 只读拒绝。blob 路径和内容不变。空间要求、完整目录备份和回退说明见[运维](operations.zh-CN.md#数据目录)。开发版本仍需完成全量验证后才能部署。没有新增必需环境变量、worker 容器或宿主权限；stacks 的未来镜像及受支持升级路径另行对齐。本次不修改 stacks，不执行部署。
