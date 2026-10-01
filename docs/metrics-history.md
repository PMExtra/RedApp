# 全局指标与历史

管理页默认展示当前值，点击指标卡片打开历史，默认近 7 天，可切换 24 小时、7 天、30 天。容量和速率使用 IEC 的 1024 换算及两位小数；未知/负值显示“—”，已观测的零值正常显示。版本与单资源继续展示当前状态和速度，不创建历史维度。指标目录固定为下表 43 项，不将版本名、资源 ID 或失败事件字符串变成时序键。

## 完整指标目录

| 分类 / 类型 | 固定键 | 单位与范围 |
| --- | --- | --- |
| 磁盘 / gauge（10） | `disk.used_bytes`、`disk.logical_bytes`、`disk.allocated_cache_bytes`、`disk.allocated_temporary_bytes`、`disk.allocated_pending_bytes`、`disk.cache_bytes`、`disk.temporary_bytes`、`disk.pending_bytes`、`disk.other_bytes`、`disk.free_bytes` | 字节；allocated/used/other 为分配块用量，cache/temporary/pending/logical 为逻辑量，free 为文件系统可用空间 |
| 请求与结果 / counter（9） | `counters.requests`、`counters.artifact_requests`、`counters.cache_hit_requests`、`counters.shared_follower_requests`、`counters.miss_requests`、`counters.reuse_requests`、`counters.download_success`、`counters.download_errors`、`counters.upstream_errors` | 累计次数；requests 包括公共业务入口、不含健康和管理 API，其它是制品下载/回源指标 |
| 流量与清理 / counter（3） | `counters.upstream_bytes`、`counters.downstream_bytes`、`counters.cleanup_freed_bytes` | 累计字节；上下游计数来自制品流，不含 metadata、安装器或管理 JSON；cleanup 是逻辑释放字节 |
| 速率 / rate（2） | `rates.upstream_bytes_per_second`、`rates.downstream_bytes_per_second` | 制品实际字节的最近五秒观测速率，单位 B/s |
| 运行 / gauge（3） | `runtime.memory_bytes`、`runtime.goroutines`、`runtime.uptime_seconds` | Go 当前分配内存字节、goroutine 数、本次进程运行秒数 |
| 资源总量 / gauge（5） | `resources.total`、`resources.current`、`resources.retired`、`resources.readers`、`resources.active_writers` | 未删除代际总数、当前代际、退役代际、读者租约、活动写入者；retired 与状态计数可能重叠 |
| 资源状态 / gauge（9） | `resources.queued`、`resources.downloading`、`resources.resuming`、`resources.retry_wait`、`resources.verifying`、`resources.complete`、`resources.failed`、`resources.invalid`、`resources.interrupted` | 各状态代际数；不包含已删除代际 |
| 发现与失败 / gauge（2） | `versions.total`、`events.recent_total` | 持久 first_seen 的版本总数、管理页近期失败列表条数（最多 100），不是失败累计总数 |

本次修正新增 miss 请求的重复计数：过去分类分支和 miss 分支各增加一次，现每个 miss 仅增加一次；不会改写已有持久累计值。

## 采样、聚合和断档

进程启动立即尝试采样，之后默认每分钟一次；不依赖管理员访问，不补写停机期间的零值。同一 UTC 分钟每个指标最多一个原始点，重复操作保留第一个点。启动不足五秒的速率窗口无效，历史留空；完整窗口中没有流量是已观测的零。

- gauge：小时保存 `min/max/avg/last/count`。平均值是实际采到的数值的算术平均，不推断缺失分钟的值，也不做时间插值。
- counter：保存累计 `last/count`，不保存累计值的 min/max/avg。`delta` 只对同一次进程内、UTC 分钟相邻、实际时间间隔大于 0 且不超过 90 秒、数值未下降的点计算；归零/下降、重启或长断档的增量未知。小时 delta 是已知增量之和，`delta_count` 和 `observed_seconds` 表示覆盖区间，不把未知区间算作零。跨小时的增量归到结束观测点所在的 UTC 小时。若只有未知区间，delta 是 null。
- rate：原始观察仅使用完整五秒窗口。每分钟采一次五秒窗口，不能当作整分钟连续观测。小时 min/max/last/count 和按观测秒数加权的 avg 只描述这些窗口，`observed_seconds = count × 5`；不是整小时流量/3600，也不是完整小时平均带宽。

计数器统计本身沿用既有持久来源，文件/数据库/网络字节并非完全原子快照；历史不参与缓存完整性、授权或发布判定。

## 保留与一致性

使用本地 SQLite 的 `metric_samples`、`metric_hours` 和 `metric_history_state`，不引入外部服务。每小时边界后下一次分钟采样汇总所有尚未提交的已结束小时。记录、聚合、推进水位和清理在一个事务内完成：先聚合，再清理，失败全部回滚。重启会先处理遗留原始点；已提交小时不会因剩余部分 raw 被再次聚合而覆盖或缩小。

分钟 raw 保留近 24 小时，小时汇总保留近 30 天，按各自 UTC 桶向下对齐保留边界。窗口也按分辨率向下对齐起点并包含当前未完桶：最多 1441 个分钟点、169 个七天小时点、721 个三十天小时点。时间槽缺失返回 null，不插值、不填零。当前桶 `partial=true`；`incomplete` 和 count 提示观察数不足。小时边界刚过而后台尚未提交时，查询临时以小时分辨率聚合待处理 raw，避免出现假缺口；查询不写数据库，不把 raw 明细传给 7d/30d 客户端。

系统时间倒退到已提交小时之前会停止该次写入/清理并记录错误，直至时间追平；不会改写已提交过去。DELETE 后 SQLite 复用空闲页，不保证数据库文件立即缩小。备份整个数据目录即可同时保留历史，沿用离线备份要求。

## API

管理会话和同源校验不变：

```sh
curl -H 'X-History-Metric: disk.cache_bytes' \
  -H 'X-History-Range: 7d' \
  --cookie admin-session.cookies \
  https://codex.example.internal/admin/api/history
```

`X-History-Range` 仅允许 `24h`、`7d`、`30d`，metric 必须在固定目录中。24h 返回原始分钟点，7d/30d 返回小时聚合（含标记的当前部分桶）；响应明确 `resolution_seconds/from/to`、类型/单位和各点覆盖信息。未知指标/范围返回 400，未登录返回 401，存储失败返回 503。沿用管理响应 no-store；公共路径的查询参数限制不变。

## 前端和验证

图表使用 MIT 许可的 uPlot 1.6.32，固定在 lockfile，生产 JS 增加约 57 KB（压缩后总 JS 约 56 KB），CSS/JS 全部本地嵌入。选择它是因为它专注于时序、支持 null 缺失点且无需大型图表框架。来源：[官方项目](https://github.com/leeoniya/uPlot)、[npm 版本](https://www.npmjs.com/package/uplot/v/1.6.32)。CSP 未放宽；Canvas 图形同时提供键盘可达的数据/覆盖表，按钮、窗口选择、关闭和 Escape 均可键盘操作，关闭后焦点回到原指标。变更窗口或关闭时取消请求，错误可重试，空历史有明确提示。

CLI 验证：`go test -race ./internal/history ./internal/store ./internal/httpserver`；`make frontend-test`；完整 `make check test build`；真实 CLI HTTP 与可选的 [无头浏览器脚本](../scripts/test-admin-headless.cjs)。聚合测试覆盖 UTC/小时边界、partial、counter 归零/重启/长间断、rate 覆盖、保留、幂等、重新打开数据库、删除故障回滚、刚关闭尚未提交的小时、未知维度拒绝和 API 分辨率。无头测试仅使用本地 fixture，不安装 Codex，不访问生产或公网下载。
