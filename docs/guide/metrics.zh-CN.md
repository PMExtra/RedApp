# 指标

[English](metrics.md)

RedApp 记录磁盘用量、流量、请求和下载资源的指标，并把历史保存在自己的数据库中，不需要外部监控服务。

## 在哪里查看指标

- **概览**（`/admin/overview`）显示全局指标：16 项常用指标，以及折叠区域中的 25 项诊断指标。
- Codex 或 Claude Code 应用的 **版本与资源** 页面显示该应用的指标。
- 点击指标可打开历史图表，可选 24 小时、7 天（默认）或 30 天。

图表按浏览器所在时区显示时间。数据以 UTC 存储和聚合。容量使用二进制单位（1 KiB = 1024 字节）。

## 指标类型

| 类型 | 含义 | 示例 |
| --- | --- | --- |
| gauge | 当前值 | 磁盘可用空间 |
| counter | 进程运行期间只增不减的累计值 | 从上游下载的字节数 |
| rate | 在 5 秒窗口内观测到的每秒字节数 | 当前下载速度 |

## 指标列表

### 磁盘（gauge，字节）

| 键 | 含义 |
| --- | --- |
| `disk.used_bytes` | 数据目录在磁盘上分配的空间 |
| `disk.logical_bytes` | 数据目录中所有文件的逻辑大小 |
| `disk.allocated_cache_bytes` | 完整缓存文件分配的空间 |
| `disk.allocated_temporary_bytes` | 进行中下载分配的空间 |
| `disk.allocated_pending_bytes` | 等待移除的文件分配的空间 |
| `disk.cache_bytes` | 完整缓存文件的逻辑大小 |
| `disk.temporary_bytes` | 进行中下载的逻辑大小 |
| `disk.pending_bytes` | 等待移除的文件的逻辑大小 |
| `disk.other_bytes` | 其他内容分配的空间，例如数据库、托管文件和图标 |
| `disk.free_bytes` | 文件系统的可用空间 |

“分配”值按磁盘块计算，“逻辑”值按文件大小计算。稀疏文件或小文件会导致两者不同。

### 请求（counter，次数）

| 键 | 含义 |
| --- | --- |
| `counters.requests` | 对应用文件、安装器和元数据的公开请求 |
| `counters.artifact_requests` | 对可下载文件的请求 |
| `counters.cache_hit_requests` | 由完整缓存副本提供的文件请求 |
| `counters.shared_follower_requests` | 加入已在进行的下载的文件请求 |
| `counters.miss_requests` | 发起新上游下载的文件请求 |
| `counters.download_success` | RedApp 完整发送给客户端的文件下载 |
| `counters.download_errors` | 发送给客户端时失败或中断的文件下载 |
| `counters.upstream_errors` | 上游连接失败和错误响应 |

健康检查和管理后台请求不计入。

### 流量与清理（counter，字节）

| 键 | 含义 |
| --- | --- |
| `counters.upstream_bytes` | 从上游来源接收的文件字节数 |
| `counters.downstream_bytes` | 发送给客户端的文件字节数 |
| `counters.cleanup_freed_bytes` | 清理移除的缓存文件逻辑字节数 |

### 速率（rate，字节/秒）

| 键 | 含义 |
| --- | --- |
| `rates.upstream_bytes_per_second` | 最近 5 秒的上游文件流量 |
| `rates.downstream_bytes_per_second` | 最近 5 秒的下游文件流量 |

### 运行时（gauge）

| 键 | 单位 | 含义 |
| --- | --- | --- |
| `runtime.memory_bytes` | 字节 | 进程当前分配的内存 |
| `runtime.goroutines` | 个 | goroutine 数量 |
| `runtime.uptime_seconds` | 秒 | 进程启动以来的时间 |

### 资源与版本（gauge，个数）

**资源**是可下载文件的一个存储副本。文件重新下载时，新副本是新的资源，旧资源被退役。

| 键 | 含义 |
| --- | --- |
| `resources.total` | 所有未删除的资源 |
| `resources.current` | 作为其文件当前副本的资源 |
| `resources.retired` | 已被替换或移除但尚未删除的资源 |
| `resources.readers` | 正在读取资源的下载 |
| `resources.active_writers` | 正在写入资源的上游下载 |
| `resources.queued` | 等待下载的资源 |
| `resources.downloading` | 正在下载的资源 |
| `resources.resuming` | 正在续传中断下载的资源 |
| `resources.retry_wait` | 失败后等待重试的资源 |
| `resources.verifying` | 正在校验的资源 |
| `resources.complete` | 已完成并通过校验的资源 |
| `resources.failed` | 下载失败的资源 |
| `resources.invalid` | 校验失败的资源 |
| `resources.interrupted` | 因停机而中断的资源 |
| `versions.total` | 已发现的发布版本；同一版本出现在两个应用中计两次 |

已退役的资源仍有状态，因此各状态计数可能与 `resources.retired` 重叠。

## 全局指标与应用指标

全部 41 项指标都有全局值。对每个应用，RedApp 记录 14 项 `resources.*` 指标、`versions.total`，以及除 `counters.requests` 外的所有 counter。磁盘、速率和运行时指标只有全局值。

- 应用指标属于应用而不是其来源。更换来源不会拆分历史。
- `versions.total` 只适用于 Codex 和 Claude Code 应用。
- HTTP 缓存应用同样记录应用指标。应用信息和托管应用没有应用指标。

## 统计哪些流量

上游和下游字节只统计**文件载荷**：

- **上游**统计从来源读取的发布文件和 HTTP 缓存文件字节。`.tar.gz` 等压缩包按压缩后的大小计算。
- **下游**统计 RedApp 交给连接的发布文件和 HTTP 缓存文件字节。反向代理之后的压缩或丢弃对 RedApp 不可见。

不统计：

- HTTP 头、TLS 和 TCP 开销、重传
- 元数据、安装脚本、图标和管理后台流量
- 托管文件的下载

其他规则：

- 同一文件的并发客户端共享一次上游下载，因此上游字节只计一次。
- 缓存命中增加下游字节，不增加上游字节。
- 失败前已读取的字节仍然计入。续传只增加实际读取的字节。
- 预热在获取时增加上游字节，但不计为客户端请求。

这些指标不是带宽或计费计量。请使用代理或网络设备进行此类统计。

## 采样与聚合

- RedApp 在启动时采样一次，之后每分钟一次，无论是否有人登录。
- counter 分批保存，可能比实际活动滞后约一秒。
- 每小时结束后，原始样本会汇总为小时数据。

| 范围 | 分辨率 | 保留 |
| --- | --- | --- |
| 24 小时 | 1 分钟（原始样本） | 原始样本保留 24 小时 |
| 7 天 | 1 小时 | 小时汇总保留 30 天 |
| 30 天 | 1 小时 | 小时汇总保留 30 天 |

小时汇总包含：

- **gauge**：所采样本的平均值、最小值、最大值和最后值
- **counter**：最后值和已知增量之和
- **rate**：最小值、最大值、最后值，以及按观测时长加权的平均值

速率样本只覆盖每分钟中的 5 秒，不是整小时的平均值。

## 缺失数据

RedApp 从不虚构数值：

- 没有样本的分钟显示为空缺，而不是零。服务停止期间会留下空缺。
- 当前小时或分钟标记为**部分**（partial）。
- 样本数少于预期的时间段标记为**不完整**（incomplete）。
- 只有同一次进程运行中、相邻分钟且间隔不超过 90 秒的两个样本之间，counter 增量才是已知的。跨重启、长时间空缺或计数下降的增量为**未知**，而不是零。
- 启动后最初 5 秒内速率未知。
- 测得的零（无流量）显示为 `0`，未知值显示为 `—`。

如果系统时钟回拨到已汇总的数据之前，RedApp 会停止写入历史，直到时钟追上。

## 备份

指标历史保存在 `state.sqlite` 中。备份数据目录即同时备份历史，见[运维](operations.zh-CN.md#备份)。配置导出不包含历史。
