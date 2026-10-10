# Metrics

[简体中文](metrics.zh-CN.md)

RedApp records metrics about disk usage, traffic, requests and download resources. It stores their history in its own database. No external monitoring service is needed.

## Where to find metrics

- **Overview** (`/admin/overview`) shows global metrics: 16 common metrics, and 25 diagnostic metrics in a collapsed section.
- The **Versions and resources** page of a Codex or Claude Code application shows the metrics of that application.
- Click a metric to open its history chart. Choose 24 hours, 7 days (default) or 30 days.

Charts show times in your browser's time zone. Data is stored and aggregated in UTC. Sizes use binary units (1 KiB = 1024 bytes).

## Metric types

| Type | Meaning | Example |
| --- | --- | --- |
| gauge | A current value | Free disk space |
| counter | A running total that only grows while the process runs | Bytes downloaded from upstream |
| rate | Bytes per second observed over a 5-second window | Current download speed |

## Metric list

### Disk (gauge, bytes)

| Key | Meaning |
| --- | --- |
| `disk.used_bytes` | Space allocated on disk by the data directory |
| `disk.logical_bytes` | Logical size of all files in the data directory |
| `disk.allocated_cache_bytes` | Space allocated by complete cached files |
| `disk.allocated_temporary_bytes` | Space allocated by downloads in progress |
| `disk.allocated_pending_bytes` | Space allocated by files waiting to be removed |
| `disk.cache_bytes` | Logical size of complete cached files |
| `disk.temporary_bytes` | Logical size of downloads in progress |
| `disk.pending_bytes` | Logical size of files waiting to be removed |
| `disk.other_bytes` | Space allocated by everything else, such as the database, hosted files and icons |
| `disk.free_bytes` | Free space available on the file system |

"Allocated" values count disk blocks. "Logical" values count file sizes. They differ for sparse or small files.

### Requests (counter, count)

| Key | Meaning |
| --- | --- |
| `counters.requests` | Public requests for application files, installers and metadata |
| `counters.artifact_requests` | Requests for downloadable files |
| `counters.cache_hit_requests` | File requests served from a complete cached copy |
| `counters.shared_follower_requests` | File requests that joined a download already in progress |
| `counters.miss_requests` | File requests that started a new upstream download |
| `counters.download_success` | File downloads that RedApp sent to a client completely |
| `counters.download_errors` | File downloads to a client that failed or were cut off |
| `counters.upstream_errors` | Upstream connection failures and error responses |

Health checks and admin console requests are not counted.

### Traffic and cleanup (counter, bytes)

| Key | Meaning |
| --- | --- |
| `counters.upstream_bytes` | File bytes received from upstream sources |
| `counters.downstream_bytes` | File bytes sent to clients |
| `counters.cleanup_freed_bytes` | Logical bytes of cached files removed by cleanup |

### Rates (rate, bytes per second)

| Key | Meaning |
| --- | --- |
| `rates.upstream_bytes_per_second` | Upstream file traffic over the last 5 seconds |
| `rates.downstream_bytes_per_second` | Downstream file traffic over the last 5 seconds |

### Runtime (gauge)

| Key | Unit | Meaning |
| --- | --- | --- |
| `runtime.memory_bytes` | bytes | Memory currently allocated by the process |
| `runtime.goroutines` | count | Number of goroutines |
| `runtime.uptime_seconds` | seconds | Time since the process started |

### Resources and versions (gauge, count)

A **resource** is one stored copy of a downloadable file. When a file is downloaded again, the new copy is a new resource and the old one is retired.

| Key | Meaning |
| --- | --- |
| `resources.total` | All resources that are not deleted |
| `resources.current` | Resources that are the current copy of their file |
| `resources.retired` | Resources replaced or removed but not yet deleted |
| `resources.readers` | Downloads currently reading a resource |
| `resources.active_writers` | Upstream downloads currently writing a resource |
| `resources.queued` | Resources waiting to download |
| `resources.downloading` | Resources downloading |
| `resources.resuming` | Resources resuming an interrupted download |
| `resources.retry_wait` | Resources waiting to retry after a failure |
| `resources.verifying` | Resources being verified |
| `resources.complete` | Resources complete and verified |
| `resources.failed` | Resources whose download failed |
| `resources.invalid` | Resources that failed verification |
| `resources.interrupted` | Resources interrupted by a shutdown |
| `versions.total` | Release versions seen; the same version in two applications counts twice |

A retired resource also has a status, so the status counts can overlap with `resources.retired`.

## Global and application metrics

All 41 metrics exist globally. For each application, RedApp records the 14 `resources.*` metrics, `versions.total` and every counter except `counters.requests`. Disk, rate and runtime metrics are global only.

- Application metrics belong to the application, not to its source. Changing the source does not split the history.
- `versions.total` applies only to Codex and Claude Code applications.
- HTTP cache applications record application metrics too. App info and hosted applications have none.

## What traffic is counted

Upstream and downstream bytes measure **file payloads** only:

- **Upstream** counts the bytes of release files and HTTP cache files read from the source. Compressed archives such as `.tar.gz` count at their compressed size.
- **Downstream** counts the bytes of release files and HTTP cache files that RedApp handed to the connection. Data that a reverse proxy compresses or drops afterwards is not visible to RedApp.

Not counted:

- HTTP headers, TLS and TCP overhead, retransmissions
- Metadata, installer scripts, icons and admin console traffic
- Hosted file downloads

Further rules:

- Concurrent clients of the same file share one upstream download, so upstream bytes are counted once.
- Cache hits add downstream bytes but no upstream bytes.
- Bytes read before a failure stay counted. A resumed download adds only the bytes it actually reads.
- Prewarm adds upstream bytes when it fetches, but does not count as client requests.

These metrics are not a bandwidth or billing measurement. Use your proxy or network equipment for that.

## Sampling and aggregation

- RedApp takes a sample at startup and then once per minute, whether or not anyone is signed in.
- Counters are saved in batches and may lag behind real activity by about a second.
- Raw samples are combined into hourly summaries after each hour ends.

| Range | Resolution | Retention |
| --- | --- | --- |
| 24 hours | 1 minute (raw samples) | Raw samples are kept for 24 hours |
| 7 days | 1 hour | Hourly summaries are kept for 30 days |
| 30 days | 1 hour | Hourly summaries are kept for 30 days |

Hourly summaries contain:

- **gauge**: average, minimum, maximum and last value of the samples taken
- **counter**: last value and the sum of known increases
- **rate**: minimum, maximum, last value and the average weighted by observed time

A rate sample covers only 5 seconds of each minute. It is not an average over the whole hour.

## Missing data

RedApp never invents values:

- Minutes without a sample show a gap, not zero. A stopped service leaves a gap.
- The current hour or minute is marked as **partial**.
- A bucket with fewer samples than expected is marked as **incomplete**.
- A counter increase is known only between two samples in the same process run, taken in consecutive minutes at most 90 seconds apart. Increases across a restart, a long gap or a counter drop are **unknown**, not zero.
- Rates are unknown during the first 5 seconds after startup.
- A measured zero (no traffic) is shown as `0`. An unknown value is shown as `—`.

If the system clock moves backward behind already summarized data, RedApp stops writing history until the clock catches up.

## Backups

Metric history is stored in `state.sqlite`. Backing up the data directory also backs up history; see [Operations](operations.md#backups). Configuration export does not include history.
