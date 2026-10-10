# Providers

[简体中文](providers.zh-CN.md)

Every application in RedApp has a **provider**. The provider decides what the application serves and how its files are fetched and cached. The provider is chosen when the application is created and cannot be changed later.

## Overview

| Provider | ID | Upstream base URL | Serves | Caching and cleanup |
| --- | --- | --- | --- | --- |
| App info | `info` | None | Details page and usage instructions | No files |
| Hosted files | `hosted` | None | Files uploaded or imported by administrators | Kept until an administrator deletes them |
| HTTP cache | `http-cache` | Required; 1–16 sources | Any file below the source URL | Path rules, default TTL 300 s, manual and automatic cleanup |
| Codex | `codex` | `https://releases.openai.com/codex` (can be changed) | Verified Codex releases and installers | Channel TTL 60 s, version cleanup, optional retention and prewarm |
| Claude Code | `claude-code` | `https://downloads.claude.ai/claude-code-releases` (can be changed) | Verified Claude Code releases and installers | Channel TTL 60 s, version cleanup, optional retention and prewarm |

All providers share the download limits from the [deployment configuration](configuration.md#field-reference).

## Vendors and applications

- Applications belong to a vendor. The public address of an application is `/<vendor>/<app>`.
- IDs use lowercase letters, digits and single hyphens, up to 63 characters. The vendor IDs `all`, `admin`, `api`, `assets` and `health` are reserved.
- IDs, the parent vendor and the provider are fixed after creation.
- Names and descriptions exist in English and Simplified Chinese. Both names are required.
- An application is public only when both the application and its vendor are enabled.
- Applications can have several categories and private tags. Tags help admin and public search but are never shown publicly.

### Deleting

- Built-in applications cannot be deleted. Disable them instead.
- Deleting a custom application is permanent. It removes the application's cache, hosted files and history, and interrupts its running downloads and tasks. Other applications are not affected.
- If the application's work does not stop within 15 seconds, the deletion stays pending. Retry it, or restart RedApp to finish it.
- A vendor can be deleted only when it has no applications.

### Changing the source

Changing the upstream source URL (or, for the HTTP cache, the list, order or strategy of sources) starts a new **source epoch**:

- New requests use only the new source. Files cached from the old source are never served for it.
- Old cache entries stay on disk. You can view and clean them up on the cache page by selecting the old source.
- Metric history continues for the same application.

## HTTP cache

The HTTP cache mirrors files from one or more upstream HTTP(S) sources.

### Path mapping

A request for `/<vendor>/<app>/<path>` fetches `<source URL>/<path>`.

| Source URL | Request | Upstream URL |
| --- | --- | --- |
| `https://files.example.internal/tools` | `/example/files/builds/tool.zip` | `https://files.example.internal/tools/builds/tool.zip` |

- Query strings, path traversal, encoded slashes and control characters are rejected.
- Clients cannot choose the upstream host.
- Client cookies and `Authorization` headers are not forwarded.
- Files are served as downloads (`attachment`), never as web pages.

### Request handling

| Request | Behavior |
| --- | --- |
| `GET`, not cached | Downloads the whole file to disk first, then starts the response |
| `GET`, cached and fresh | Served from disk without contacting the source |
| `GET`, cached and expired | Revalidated with the source (`ETag`, `Last-Modified`); `304` keeps the copy, `200` replaces it |
| `HEAD`, not cached | Sends `HEAD` to the source; nothing is stored |
| Single byte range | Served from the complete file (`206`); a cold range request downloads the whole file first |
| Multiple byte ranges | Ignored; the full file is returned |
| Conditional request | `If-None-Match`, `If-Modified-Since`, `If-Match` and `If-Unmodified-Since` are supported |

Several clients requesting the same missing file share one upstream download.

Client request cache directives (`Cache-Control: no-store`, `no-cache`, `max-age`, `Pragma`) are ignored: they never force an upstream fetch, and freshness is governed only by rules and upstream headers. `only-if-cached` is honoured and returns `504` on a miss.

If the source marks a file as uncacheable, each concurrent client gets its own direct transfer, limited by the writer limit (`503 TRANSFER_CAPACITY` when exhausted). `503 CACHE_CONTENDED` is returned only when the cached copy keeps changing during a request.

### Freshness

The TTL decides how long a cached file is fresh. It is **not** a retention period: expired files stay on disk until cleanup removes them.

RedApp picks the TTL in this order:

1. **Matching cache rule.** The first rule whose pattern matches the path sets the TTL. Upstream `Cache-Control`, `Age` and `Expires` are ignored.
2. **Upstream `Cache-Control`.** Without a matching rule, RedApp uses `s-maxage`, or else `max-age`, minus the age reported by `Age` or `Date`. If `Cache-Control` is present but has no valid lifetime, the TTL is 0.
3. **Application default TTL.** Used only when the response has no `Cache-Control` at all. The default is 300 seconds.

`Expires` is never used. Directives such as `no-cache` or `must-revalidate` from the source do not shorten the computed lifetime.

**TTL 0** means every request revalidates with the source first. The file is still stored, so it can serve as a fallback when the source fails.

### Path rules

Rules match the decoded path relative to the application, starting with `/`. For `/example/files/releases/tool.zip`, the path is `/releases/tool.zip`.

| Pattern | Type | Matches |
| --- | --- | --- |
| `/` | glob | Every file |
| `/releases` | glob | The file `/releases` and everything below the directory `/releases/` |
| `/releases/` | glob | Only files below the directory `/releases/` |
| `/**/*.zip` | glob | Any `.zip` file, at any depth |
| `/releases/[^/]+\.zip` | re2 | `.zip` files directly in `/releases/`; the whole path must match |

- **glob** uses doublestar syntax (`*`, `**`, `?`, `[...]`, `{a,b}`). A pattern that matches a directory also matches every file below it.
- **re2** is a Go RE2 regular expression that must match the whole path. Back-references and lookaround are not supported.
- Rules are checked in order and the **first match wins**. Reordering rules changes the result.
- Use **Test match** in the cache settings to check a pattern against a path.

Example policy as stored in an exported configuration:

```yaml
http_policy:
  stale_fallback: true
  rules:
    - match: {type: glob, pattern: /releases/}
      ttl_seconds: 3600
    - match: {type: re2, pattern: '/channels/[^/]+\.json'}
      ttl_seconds: 0
  auto_cleanup:
    - match: {type: glob, pattern: /releases/}
      basis: last_access
      age_seconds: 2592000
```

### Responses that are not shared

Some upstream responses must not be given to other clients. RedApp streams them directly to the requesting client without storing them:

- `Cache-Control: no-store` or `private`, unless an explicit rule matches the path
- Responses with `Set-Cookie`
- Responses with `Vary` on anything other than `Accept-Encoding`

A matching cache rule deliberately overrides `no-store` and `private`, so the file is cached and shared at the public download address. The `Set-Cookie` and `Vary` boundaries still apply and cannot be overridden by rules.

When a rule with a TTL above 0 overrides upstream `no-store` or `private`, RedApp records a warning event for each upstream fetch.

### Stale fallback

`stale_fallback` is on by default.

- When all sources fail with a network error, a timeout or a `5xx` status, RedApp serves the expired cached copy instead of an error.
- Only a complete copy from the same application and the same source epoch is used. There is no maximum age.
- Each fallback records a warning event.
- `404` and `410` are not failures; they are returned to the client.
- With `stale_fallback` off, the error is returned.

### Multiple sources

An HTTP cache application can list 1 to 16 source URLs.

| Strategy | Behavior |
| --- | --- |
| `ordered` (default) | Try sources in the listed order |
| `round_robin` | Start at the next source each time, then try the rest |
| `random` | Try sources in random order, each at most once |

- RedApp moves to the next source only on a network error, a timeout or a `5xx` status.
- An attempt is abandoned when no data arrives for 60 seconds. One download may take up to 9 minutes across all sources, so very large files on slow links can fail.
- A file is always taken from a single source. Partial downloads are never combined across sources.
- Validators (`ETag`, `Last-Modified`) are reused only with the source that issued them.
- There are no health checks or weights. Fresh cache hits do not advance `round_robin`.

### Refresh

On the cache page you can force a revalidation:

- **Refresh one file** revalidates a single cached path, even if it is still fresh.
- **Refresh by pattern** builds a preview of the matching cached files. Executing it revalidates all of them in the background.

Refresh only touches files already in the cache. It does not discover new upstream files; use prewarm for that.

### Manual cleanup

Manual cleanup removes cached files that match a pattern and are older than a cutoff time.

1. Choose a pattern, a time basis and a cutoff time.
   - `fetched_at` is when the file body was last downloaded.
   - `last_access` is when a client last used the file. It is tracked per minute. Files never accessed use `fetched_at`.
2. Review the preview. It lists the exact files to remove.
3. Execute. RedApp removes exactly the previewed files, skipping any that were used after the preview.

Files being downloaded are removed after their downloads finish.

### Automatic cleanup

Automatic cleanup is off until you save at least one rule.

- Each rule has a pattern, a basis (`fetched_at` or `last_access`) and a minimum age.
- The **first** rule whose pattern matches a file decides. If that file is not old enough yet, later rules are not tried.
- RedApp runs automatic cleanup every 15 minutes, starting 15 minutes after startup.
- Each pass scans at most 1,000 files and removes at most 100 per application. The next pass continues where the last one stopped.
- Only the current source epoch of enabled applications is processed. Old epochs need manual cleanup.

### Previews and receipts

| Item | Value |
| --- | --- |
| Preview validity | 10 minutes |
| Previews being built at once | 8 |
| Items per page | 25 by default, up to 100 |
| Receipt kept after execution | 24 hours |

A restart interrupts a running refresh or cleanup. It does not resume; create a new preview.

### Limits

| Setting | Range |
| --- | --- |
| Application default TTL | 0–86,400 seconds (default 300) |
| Rule TTL | 0–86,400 seconds |
| Cache rules | Up to 32 |
| Automatic cleanup rules | Up to 32 |
| Pattern length | 1–1,024 bytes |
| Cleanup minimum age | 60–315,360,000 seconds |
| Sources | 1–16 |

## Codex and Claude Code

These providers serve official releases. RedApp fetches metadata and binaries from the upstream, verifies them, caches them and serves them together with patched installers. See [Security](security.md#upstream-verification) for the checks.

### Channels

| Provider | Channels |
| --- | --- |
| Codex | `latest` |
| Claude Code | `latest`, `stable` |

A channel points to a version. RedApp caches the channel answer for the **channel TTL**: 60 seconds by default, 1–86,400 seconds allowed. Released versions do not change, so their files are cached until cleanup.

### Installers

Each application provides `install.sh` and `install.ps1` at `/<vendor>/<app>/`. The application page shows the commands with your public address. The installers download from RedApp, not from the upstream.

### Version cleanup

On the cache page, choose a minimum version. The preview lists cached versions below it; executing removes them. Version metadata and metric history are kept.

- Versions that RedApp cannot compare are listed in the preview and kept.
- Executing removes exactly the previewed files. Files being downloaded or read are removed when their transfers finish.
- A preview expires after 10 minutes. If the source changed meanwhile, nothing is removed; create a new preview.
- Earlier source epochs can be cleaned the same way.

### Version retention

Retention keeps only the newest cached versions. It is off by default.

- Set **keep latest** to N (1–1,000, default 3).
- RedApp counts only versions with at least one complete cached file in the current source.
- Besides the newest N, RedApp always keeps versions that a channel currently points to, versions being downloaded or read, and versions it cannot compare.
- If a channel cannot be verified, the whole pass is skipped.
- Retention runs every 15 minutes and removes at most 100 versions per application per pass. You can also preview and run it manually.
- A manual preview uses the saved setting, so save changes first. It expires after 10 minutes; after execution, its result stays available for 24 hours.
- Old source epochs and disabled applications are not touched.

### Prewarm

Prewarm downloads files before clients ask for them.

| Platform list | Values |
| --- | --- |
| Codex (6) | `darwin-arm64`, `darwin-x64`, `linux-arm64`, `linux-x64`, `win32-arm64`, `win32-x64` |
| Claude Code (8) | `darwin-arm64`, `darwin-x64`, `linux-arm64`, `linux-x64`, `linux-arm64-musl`, `linux-x64-musl`, `win32-arm64`, `win32-x64` |

- **Automatic prewarm** (release applications only): choose channels and platforms. Every 15 minutes, RedApp downloads the versions those channels point to. Versions already prewarmed are skipped; failures are retried in the next pass.
- **Manual prewarm** (release and HTTP cache applications): start a one-time task. For the HTTP cache, provide file paths, a UTF-8 path list, or directory paths whose listings RedApp crawls. Directory listings in HTML, nginx `autoindex` JSON and Caddy `browse` JSON are supported.

| Limit for a prewarm task | Default | Maximum |
| --- | --- | --- |
| Files | 10,000 | 100,000 |
| Directory depth | 16 | 32 |
| Bytes read | 10 GiB | 1 TiB |
| Duration | 1 hour | 24 hours |

- Only one prewarm task runs at a time in the whole service. There is no queue; a second start is refused while one runs.
- Disabled applications cannot be prewarmed.
- Each directory listing may be up to 2 MiB.
- Cancelling a task does not cancel downloads that public clients also wait for.
- A restart marks a running task as interrupted. It is not resumed.
- Finished tasks are kept for 24 hours.
- Prewarm does not count as public downloads in metrics or rankings. Bytes actually fetched from upstream are counted.

## Hosted files

Hosted applications serve files that administrators provide.

- **Upload** a file from the browser, or **import** it once from an `http` or `https` URL.
- Each file has a path inside the application, for example `tools/setup.exe`, and is served at `/<vendor>/<app>/tools/setup.exe`.
- The size limit per file is `max_artifact_bytes` (4 GiB by default).
- Files are kept until an administrator deletes them. To change a file, use **Replace** on it; a conflicting change made meanwhile is rejected.
- Uploads and imports show their progress and can be cancelled while they run.
- An import URL may contain a query string, for example a signed download link. RedApp uses it once and never stores or shows it. URLs with credentials are refused.
- Downloads support byte ranges and conditional requests.

## App info and usage instructions

An **App info** application has only a details page and usage instructions. It serves no files. Every other provider also has usage instructions.

Usage instructions are written per language in Markdown. Raw HTML, JavaScript and external resources are allowed. They are displayed in a sandboxed frame; see [Security](security.md#usage-instructions-sandbox).

Built-in Codex and Claude Code applications come with default instructions from their presets. Saving empty instructions keeps them empty; it does not restore the default. Use **Reset** to return to the preset text.

### Template variables

Variables are replaced when the page is shown. Values are HTML-escaped. Unknown variables are left unchanged.

| Variable | Value | Example |
| --- | --- | --- |
| `{{base_url}}` | Effective public address, without trailing `/` | `https://downloads.example.internal` |
| `{{public_origin}}` | Same as `{{base_url}}` | `https://downloads.example.internal` |
| `{{app_path}}` | Application path | `/openai/codex` |
| `{{app_key}}` | Application key | `openai/codex` |
| `{{app_url}}` | Full application address | `https://downloads.example.internal/openai/codex` |
| `{{app_name}}` | Application name in the page language | `Codex` |
| `{{latest_version}}` | Newest version known in the current source, or `<version>` if none | `0.50.0` |

Example in Markdown:

```sh
curl -fsSL '{{base_url}}{{app_path}}/install.sh' | sh
```
