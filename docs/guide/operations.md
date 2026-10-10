# Operations

[简体中文](operations.zh-CN.md)

This guide covers running RedApp in production: containers, data, reverse proxies, health checks, the admin console, backups and troubleshooting. For settings, see [Configuration](configuration.md).

## Docker

The image is built from `scratch`. It contains only the `redapp` binary and the CA certificate bundle.

| Property | Value |
| --- | --- |
| User | UID/GID `65532` |
| Port | `8080` |
| Data volume | `/var/lib/redapp` (mode `0700`) |
| Command | `/redapp serve` |
| Health check | `/redapp healthcheck` every 30 s, timeout 5 s, 3 retries |
| Optional config file | `/etc/redapp/config.yaml` |

Recommended run command:

```sh
docker build -t redapp:local .
docker run -d --name redapp --read-only \
  -p 127.0.0.1:8080:8080 \
  -v redapp-data:/var/lib/redapp \
  -e REDAPP_PUBLIC_URL=https://downloads.example.internal \
  redapp:local
```

- RedApp writes only to the data directory, so `--read-only` works.
- A bind-mounted data directory must be writable by UID/GID `65532`. RedApp does not change ownership and does not fall back to another directory.
- To use a config file, mount it read-only at `/etc/redapp/config.yaml`, or mount it elsewhere and set `REDAPP_CONFIG` to its container path.

### Health check arguments

The image health check runs `/redapp healthcheck` without arguments. Docker does not pass the container command's arguments to it.

Prefer environment variables and config files: both the server and the health check read them. If you override the command with flags such as `--listen`, give the health check the same flags.

The image has no shell, so the health check must use the exec form. `docker run --health-cmd` uses the shell form and does not work with this image. Use Docker Compose instead:

```yaml
services:
  redapp:
    image: redapp:local
    read_only: true
    command: ["serve", "--listen", ":9090"]
    healthcheck:
      test: ["CMD", "/redapp", "healthcheck", "--listen", ":9090"]
    volumes:
      - redapp-data:/var/lib/redapp
volumes:
  redapp-data:
```

`redapp healthcheck` connects to the configured listener on the loopback address and requests `/health/ready`. It ignores the public address and any outbound proxy.

## Data directory

The data directory holds all runtime state:

| Path | Content |
| --- | --- |
| `state.sqlite` (with `-wal` and `-shm`) | Configuration, accounts, cache index, metrics, events |
| `objects/` | Cached release files, HTTP cache bodies, hosted files, icons |
| `instance.lock` | Lock that prevents a second instance |

- One directory serves one instance. Network file systems (NFS, SMB) are not supported.
- Never delete `instance.lock`. The operating system releases it when the process exits.
- The directory must be an absolute path. RedApp creates it with mode `0700` if it does not exist.

Until version 1.0, data directories are not compatible across database schema versions: a newer build refuses a data directory written by an older one, and there are no migrations. Start a new build with a new empty data directory, and keep the old directory if you may need to go back.

A refused directory is never modified.

## Reverse proxy

Run RedApp behind an HTTPS reverse proxy for anything beyond local testing.

1. Terminate TLS at the proxy and forward to RedApp's listener.
2. Restrict accepted domain names at the proxy. RedApp accepts any syntactically valid `Host`.
3. Make the proxy overwrite the forwarding headers sent by clients.
4. Add the proxy's address range to `trusted_proxies`.
5. Disable response buffering and allow long transfers, because downloads can be large. RedApp has no total time limit for a download; it ends one only when the client accepts no data for 60 seconds.

Example for nginx:

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host downloads.example.internal;
    proxy_set_header Forwarded "for=$remote_addr;proto=https;host=downloads.example.internal";
    proxy_set_header X-Forwarded-For "";
    proxy_set_header X-Forwarded-Proto "";
    proxy_set_header X-Forwarded-Host "";
    proxy_buffering off;
    proxy_request_buffering off;
    proxy_read_timeout 600s;
    client_max_body_size 0;
}
```

```sh
REDAPP_TRUSTED_PROXIES=127.0.0.1/32 redapp serve
```

How RedApp reads forwarding headers:

- Headers are used only when the direct peer is in `trusted_proxies`.
- A valid `Forwarded` header takes precedence over `X-Forwarded-*`.
- The chain is read from right to left until the first untrusted address.
- Malformed headers from a trusted proxy produce `400 Bad Request`.

Set `REDAPP_PUBLIC_URL` (or the override in the console) so that install commands use your public HTTPS address.

## Health endpoints

| Endpoint | Checks | Success |
| --- | --- | --- |
| `GET /health/live` | The process answers HTTP | `200` |
| `GET /health/ready` | The database responds and the data directory is writable, checked at most once every 5 seconds | `200`; otherwise `503` |

Neither endpoint contacts upstream sources, so an internet outage does not mark RedApp unhealthy. Health requests are not counted in metrics.

## Admin console

Sign in at `/admin/login`. Pages that you can open directly or bookmark:

| Path | Page |
| --- | --- |
| `/admin/overview` | Overview: service status and metrics with history charts |
| `/admin/events` | Events: recent warnings and failures |
| `/admin/settings/site` | Site settings: titles, subtitles, disclaimers, public address, homepage pins |
| `/admin/settings/proxy` | Upstream proxy: global outbound proxy |
| `/admin/vendors` | Vendors and applications: list, search, import |
| `/admin/vendors/new` | Create a vendor |
| `/admin/categories` | Rename categories |
| `/admin/vendors/<vendor>` | Opens the vendor settings |
| `/admin/vendors/<vendor>/settings` | Vendor settings, export |
| `/admin/vendors/<vendor>/apps` | Applications of a vendor |
| `/admin/vendors/<vendor>/apps/new` | Create an application |
| `/admin/vendors/<vendor>/admin-notes` | Private notes for a vendor |
| `/admin/vendors/<vendor>/apps/<app>` | Opens the application's main tab: versions (Codex, Claude Code), cache (HTTP cache), files (Hosted) or settings |
| `/admin/vendors/<vendor>/apps/<app>/settings` | Application settings, usage instructions, export, copy |
| `/admin/vendors/<vendor>/apps/<app>/admin-notes` | Private notes for an application |
| `/admin/vendors/<vendor>/apps/<app>/versions` | Versions and resources (Codex, Claude Code) |
| `/admin/vendors/<vendor>/apps/<app>/cache` | Cache management (HTTP cache, Codex, Claude Code) |
| `/admin/vendors/<vendor>/apps/<app>/files` | Hosted files (Hosted provider only) |

Public pages:

| Path | Page |
| --- | --- |
| `/` | Home: pinned applications and a 7-day download ranking |
| `/all` | Catalog of all published applications, with search, categories and paging |
| `/<vendor>` | Vendor page with its published applications |
| `/<vendor>/<app>` | Application page with usage instructions |
| `/<vendor>/<app>/<path>` | Files, installers and release downloads |

The home page shows the pinned applications in the order set on the site settings page (up to 100). Only published applications appear there. Pins of disabled or deleted applications stay listed and labelled in the admin console until you remove them.

The vendor IDs `all`, `admin`, `api`, `assets` and `health` are reserved.

## Background maintenance

These tasks run inside the server. Each runs every 15 minutes. None runs at startup; the first run happens 15 minutes after the server starts.

| Task | What it does |
| --- | --- |
| HTTP cache cleanup | Applies automatic cleanup rules of HTTP cache applications |
| Release retention | Keeps the latest N cached versions of Codex and Claude Code applications, if enabled |
| Automatic prewarm | Downloads new releases for configured channels and platforms, if enabled |
| Preview expiry | Removes expired cleanup, retention and refresh previews and old results |

Metric sampling runs once at startup and then every minute.

## Logs and events

RedApp writes structured log lines to standard error in `key=value` text format. Every line has `time`, `level` (`DEBUG` is not shown, then `INFO`, `WARN`, `ERROR`) and `msg`. Lines from background tasks also have `component`:

```text
time=2026-10-10T08:00:00.000Z level=INFO msg="RedApp started" version=1.0.0 listen=:8080 data_dir=/var/lib/redapp
time=2026-10-10T08:15:00.000Z level=WARN msg="automatic HTTP cache cleanup failed" component=http_cache app=example/files error="database is locked"
```

| `component` | What is logged |
| --- | --- |
| (none) | Startup and shutdown, `http request` access lines, `request failed` lines, the fatal error when startup fails |
| `auth` | The initial admin password, once, on the first start |
| `configuration` | Every published configuration change, with the number of applications and sources |
| `download` | Cache recovery at startup, failed release downloads and failed state writes |
| `http_cache` | Failed automatic cleanup passes, failed or stopped cache refresh jobs, cleanup and refresh results that could not be saved |
| `hosted` | Removal of incomplete uploads at startup, uploaded files that could not be removed |
| `prewarm` | One line per finished prewarm job, failed automatic prewarm starts |
| `retention` | Retention runs that retired versions, skipped runs and failures |
| `history` | Failed metric sampling |
| `counters` | The first failed counter flush and the recovery after it |

Common fields:

- `request_id`: the request; HTTP lines only
- `app`: the application as `<vendor>/<app>`
- `storage_id`: the internal cache namespace of an application (`app/<uid>-e<epoch>`), in `download` lines
- `job_id`, `preview_id`, `generation_id`, `transfer_id`: the prewarm job, cleanup preview, download generation or upload
- `state`, `reason`, `outcome`: the result of a job or run
- `error`: the underlying error; credentials in URLs are masked

Background tasks log failures and results, never one line per request or per file. A failure that repeats every second, such as a counter flush, is logged once until it recovers. Apart from the initial admin password, logs contain no passwords, session or CSRF tokens, or proxy credentials.

Every response carries an `X-Request-Id` header, and error responses repeat it as `error.request_id`. Search the log for that value when a user reports an error.

Operational warnings go to the **Events** page instead of the log. Examples are stale cache fallback, cache rules that override upstream `no-store`, upstream errors, failed downloads and failed automatic cleanup. RedApp keeps the newest 1,000 events for up to 30 days.

## Backups

Back up the whole data directory. It contains configuration, accounts, cached files, hosted files and history.

1. Stop the service so that the database is consistent.
2. Copy the entire data directory, including `state.sqlite-wal` and `state.sqlite-shm` if present.
3. Start the service again.

Do not copy `state.sqlite` alone while the service runs. Treat backups as sensitive: they contain the admin password hash and outbound proxy credentials.

To restore, stop the service and replace the data directory with the backup. A backup works only with a build that uses the same database schema.

A [configuration export](configuration.md#export) is a lightweight alternative for settings only. It does not contain cached files, hosted files, history or accounts.

## Troubleshooting

| Symptom | Likely cause and action |
| --- | --- |
| Startup fails with "belongs to another RedApp schema version" | The data directory comes from another schema version. Use a new empty directory. |
| Startup fails with "already owned by another instance" | Another process uses the directory. Stop it. Do not delete `instance.lock`. |
| Startup fails with a permission error | The directory is not writable by the service user (`65532` in Docker). Fix ownership. |
| Container is unhealthy after changing flags | The health check does not see your flags. Use environment variables, or pass the same flags in an exec-form health check. |
| Install commands show `http://` or an internal host name | Set `REDAPP_PUBLIC_URL` or the public address override, and check `trusted_proxies`. |
| Admin sign-in fails behind the proxy | The forwarded scheme or host does not match the browser address. Check the proxy headers and `trusted_proxies`. |
| Downloads fail with `503` | Download capacity is full. Raise `max_writers` or `max_readers`, or retry later. |
| Downloads fail with `502` | The upstream is unreachable or returned untrusted metadata. Check the outbound proxy and the Events page. |
| An application returns `404` | The vendor or the application is disabled. Enable both. |
| Everyone is signed out after a restart | Expected. Admin sessions are kept in memory only. |
| Lost the admin password | Restore a backup, or start with a new empty data directory. There is no reset command. |
