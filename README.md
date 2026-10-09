# RedApp

[Chinese](README.zh-CN.md)

RedApp distributes **HTTP files, Codex CLI and Claude Code** through one service. Administrators manage vendors and applications, choose a provider and upstream base URL, and inspect cache, traffic and service settings.

**0.8.1 uses SQLite schema 11.** Start with a new empty directory/volume, or reuse an exact schema-11 directory. Released 0.8.0 (schema 10), older schemas and unknown directories are refused without migration, rewrite or deletion. Keep the original directory unchanged to switch back to its matching older program. The deployment file format remains schema_version 1. See the [configuration contract](docs/configuration-v0.8.0.md), [administration, categories and tags](docs/admin-taxonomy-v0.8.1.md) and [acceptance status](docs/acceptance.md).

## Start the service

No configuration file is required. The native executable accepts `redapp` or `redapp serve`; the image starts `serve` and its health check resolves the same configuration.

```sh
docker build -t redapp:local .
docker run -d --name redapp --read-only \
  -p 127.0.0.1:8080:8080 \
  -v redapp-v08-data:/var/lib/redapp \
  redapp:local
```

Defaults are `:8080`, `/var/lib/redapp`, and no trusted proxies. RedApp accepts any syntactically valid request Host; configure domain and network access policy at the reverse proxy. Use a new empty directory/volume for this schema, when moving from any older schema. Directory permission failures never fall back elsewhere.

Deployment environment variables work without a file, for example:

```sh
REDAPP_DATA=/absolute/writable/redapp-data \
REDAPP_LISTEN=127.0.0.1:8080 \
  redapp
```

Configuration path selection is **`--config FILE` > `REDAPP_CONFIG` > `/etc/redapp/config.yaml`**. Only the selected file is read. The default YAML file is loaded when present and may be absent; any manually selected missing file, or any selected invalid/unreadable file, stops startup. An empty `REDAPP_CONFIG` is unset. There is no automatic discovery of `config.json` or a working-directory config file. Existing JSON deployments can set `REDAPP_CONFIG=/etc/redapp/config.json` to keep both service and health check on that file.

The optional [YAML example](config/example.yaml) uses defaults; omit any fields you do not need. Explicit `.json` files remain supported ([JSON example](config/example.json)). Deployment field priority is **CLI options > environment > selected file > built-in defaults**. Lists replace lower-priority lists. Each supplied source is validated, so an invalid selected file is not hidden by an override. Files must be a single mapping with known, correctly typed fields; duplicate keys, nulls, YAML anchors/aliases/merge keys and invalid values are rejected. Validation does not create or open the data directory.

```sh
redapp config validate
redapp config validate --config ./config.yaml
REDAPP_CONFIG=/etc/redapp/config.json redapp serve
redapp serve --config ./custom.yaml --listen 127.0.0.1:8081
```

For an optional container YAML file, add `--mount type=bind,src=/absolute/config.yaml,dst=/etc/redapp/config.yaml,readonly` to `docker run`. For another YAML/JSON path, mount that file and set `REDAPP_CONFIG` to its container path. If instead you override the container command with `--config` or deployment flags, supply equivalent flags in `--health-cmd`; Docker does not forward CMD arguments to HEALTHCHECK. See the [deployment field/env/CLI table](docs/operations.md#启动配置) for all supported options and limits. Providers and release trust policies remain compiled definitions; each application has an administrator-configured BaseUrl.

Open **http://localhost:8080/** for the catalog or **http://localhost:8080/admin/overview** for administration. The first-start logs contain the initial admin password; protect those logs and change the password after signing in. Containers run as UID/GID 65532; ensure bind-mounted data directories are writable by that identity.

Starting with v0.6.3, RedApp accepts capacity strings such as `REDAPP_MAX_ARTIFACT_BYTES=4GiB` through environment, CLI and YAML/JSON configuration, alongside integer byte counts. This is a **per-file** limit, not total cache capacity: `4GB`/`4gb` means 4,000,000,000 bytes, while `4GiB` means 4,294,967,296 bytes. The default remains 4 GiB; the accepted range is 1 byte to 1 TiB. v0.6.2 requires integer bytes; see [release notes](docs/releases.md).

Writer limits use `REDAPP_MAX_WRITERS`, `download_limits.max_writers`, or `--max-writers`; reader names remain unchanged. All three providers share these limits. Removed Host and writer options have no aliases.

## Providers and application management

Open `/admin/vendors` to add a vendor and its applications. Both have lowercase IDs, English/Simplified Chinese names and descriptions, and optional JPG, PNG or static SVG icons. IDs, parent vendor and provider are fixed after creation. Display fields, BaseUrl, cache TTL and enabled state can be edited with revision checks.

| Provider | BaseUrl | Cache and cleanup |
| --- | --- | --- |
| App Info (`info`) | None | Details and bilingual Markdown/HTML/JavaScript usage instructions; no file routes |
| Hosted Files (`hosted`) | None | Administrator uploads or one-time URL imports; permanent local downloads until manual deletion |
| HTTP Cache (`http-cache`) | Required; HTTP(S), ports and internal sources supported | Ordered path TTL rules; 300-second default when no rule or Cache-Control applies; manual or optional automatic cleanup by fetched time or last access |
| Codex (`codex`) | Defaults to `https://releases.openai.com/codex`; overridable | Verified release metadata/artifacts; channel TTL default 60 seconds; version cleanup |
| Claude Code (`claude-code`) | Defaults to `https://downloads.claude.ai/claude-code-releases`; overridable | Existing signed metadata and digest checks; channel TTL default 60 seconds; version cleanup |

Missing built-in vendor/application templates are inserted disabled on startup; existing full-key records and explicit blank instructions are preserved. Enable both the vendor and its application to make it public. IDs are scoped as `vendor_id/app_id`; `all` is reserved for the public directory. Built-in application keys cannot be deleted even when their provider differs. Custom applications can be permanently deleted after confirmation, including owned files, cache and history; deletion interrupts only that application’s downloads, uploads and background work, waits for their handles to close, and has no undo. A timed-out deletion remains blocked and can be retried or resumed on restart. Shared uploaded icons remain independent. A BaseUrl change creates a separate source epoch and retains older cache for explicit management. Template-bound fields show a Reset button only when overridden or edited; Reset removes that field's override on the next revision-checked save and never removes stored files or history.

The homepage shows ordered administrator pins and an approximate rolling seven-day download-client ranking. `/all` supports shareable search and pagination; `/<vendor>` shows vendor details and enabled applications. Navigation search offers bounded vendor/application suggestions. Instructions run as trusted HTML documents, including administrator-authored JavaScript and external resources, under a separate policy; see the runtime guide for the trust boundary.

HTTP Cache maps `/<vendor>/<app>/<relative-path>` below its configured BaseUrl. It supports GET/HEAD, validators and single byte ranges, and rejects query strings and path traversal. Cacheable cold downloads are fully spooled before the response starts, including cold range requests. Responses that cannot be shared use a bounded direct stream. Downloaded files are attachments rather than executable pages.

HTTP Cache supports 1–16 ordered mirror URLs with ordered fallback, round robin or random selection. Source edits use a new cache epoch; validators stay bound to the actual source and cross-source retries restart the whole file. Admins can refresh one cached resource or a pattern. Refresh and cleanup previews use server-side pagination, while execution processes the full frozen selection.

HTTP Cache settings support ordered cache rules, a `stale_fallback` switch (default `true`), and optional automatic-cleanup rules. Matching uses decoded application-relative paths with a leading `/`: doublestar globs match the file or a directory ancestor, while Go RE2 expressions match the whole path. The first matching rule wins. An explicit rule's TTL overrides source Cache-Control and Age without an application-default cap; otherwise, valid source `s-maxage`/`max-age` determines freshness with Age/Date deducted. The application TTL is only the fallback when Cache-Control is absent; Cache-Control without a valid lifetime means TTL 0. Expires adds no separate priority. TTL 0 always contacts the source first but may retain a complete body for failure fallback; there is no separate bypass mode.

An explicit path rule may intentionally override source `no-store`/`private` and reuse that representation at the public download endpoint. Existing Set-Cookie, unsupported-Vary and authorization representation boundaries still apply. Actual upstream overrides with TTL greater than 0 produce one warning per fetch; TTL 0 suppresses that warning. On network failure, timeout or upstream 5xx, `stale_fallback=true` serves an available complete expired representation from the same app/source epoch, with no maximum stale age. Each actual fallback records one warning per shared upstream fetch, with no deduplication or rate limit; cache hits do not warn. Setting the switch to `false` returns an error instead. Source revalidation directives do not force extra validation or prohibit fallback, while ETag/Last-Modified conditional validation remains supported. Fallback does not advance timestamps; 404/410 do not trigger it. Finer controls, presentation and observability remain [in issue #2](https://github.com/PMExtra/RedApp/issues/2).

Manual cleanup combines a path pattern, time basis and cutoff in a frozen preview. Automatic cleanup is disabled until rules are saved, then runs every 15 minutes without an immediate startup deletion. It only processes active HTTP Cache applications' current source epochs, scanning at most 1,000 files and retiring at most 100 per application per pass; cursors prevent starvation. A cleanup rule's first path match owns the file even when its age threshold is not yet met. See the [runtime guide](docs/provider-runtime-v0.7.0.md) for rule examples, limits and API details. New source authentication, private CA and signing options are outside this version; normal TLS certificate verification and existing administrator/release protections remain in place.

## Configuration, maintenance and portability

Trusted defaults are embedded from `presets/<vendor>.yaml`, `presets/<vendor>/<app>.yaml` and `presets/_taxonomy.yaml`. Bound objects save a template reference and sparse explicit overrides; independent objects save a complete spec. Equal custom values, explicit blanks and empty lists stay custom. Language fields inherit independently; ordered lists and proxy/prewarm/retention replace whole values. A field's Reset removes only that override; enabled, identity, Provider and private notes remain separate.

Codex/Claude retention keeps the latest N complete cached versions in the current source, plus fresh channel targets, active readers/writers and incomparable versions. Historical sources are preserved. Release-platform and HTTP path/list/directory prewarm share the existing verified catalog/download/cache pipeline. Maintenance runs every 15 minutes without immediate startup work. Manual prewarm uses one global worker with no queue; defaults are 10,000 files, depth 16, 10 GiB and one hour, with hard limits 100,000/32/1 TiB/24 hours. Cancellation removes its shared waiter while public requests can continue; restart marks work interrupted. Applications can belong to several bilingual categories; new categories are typed in the editor and created with the save, and unused custom categories are removed automatically. Tags are private free text that search matches but public pages never show. `/all` lists categories with site-wide public counts.

Administration can export ZIP or import ZIP/single YAML in linked or independent mode. Export includes selected configuration, parent Vendor, referenced category names and validated static image assets. It excludes Hosted binaries, runtime IDs/state/cache/history/tasks; notes and proxy credentials default off each time. Import previews source text without running HTML/JS and requires explicit trust for changed instructions. Choices, UID/revisions and notes are fenced; the whole configuration transaction either commits or rolls back. Copy creates a disabled App with fresh UID/epoch and no old data; its own `inherit` proxy follows the target Vendor. Successful receipts last 24 hours and return the same committed result after administrator re-login, while unexecuted previews expire on restart/session change. See the [full fields, limits and API contract](docs/configuration-v0.8.0.md).

## Public address and proxy trust

The global public address determines generated download links and installers. Its priority is:

1. The saved administrator override in site settings.
2. The deployment environment variable `REDAPP_PUBLIC_URL`.
3. The current request origin, derived from a valid Host and trusted proxy headers.

Clearing the administrator override restores the environment default, if present. The interface shows the effective address and its source. Only HTTP(S) origins are accepted, with no credentials, subpath, query, or fragment. An invalid nonempty environment value prevents startup. Changing the public address only affects generated links; it preserves request-origin checks, cookie security, and upstream download authorization.

For enterprise access, use an HTTPS reverse proxy. Configure the proxy's CIDRs in `trusted_proxies` and make the proxy overwrite forwarded headers. Headers from untrusted peers are ignored. RedApp validates Host syntax and derives the request origin from the trusted proxy chain; the proxy controls accepted domains. Health checks connect to the configured local listener, independently of the public address.

Site branding and public address are global. Outbound proxy resolves global → Vendor → App: `inherit` uses the parent, `direct` stops inheritance, and `url` supplies a complete HTTP(S)/SOCKS5 proxy URL. Vendor/App proxy is an atomic configuration leaf; changing it does not silently rewrite a child override. Global settings use an empty server for direct access. Complete URLs and their percent-encoded user information are available only through protected administrator APIs; public responses, logs and events exclude credentials. Configuration commits use revision/CAS and prepare transports before publishing; started readers retain their previous transport. There is no old credential migration.

## Install applications

Replace `downloads.example.internal` with your service address. Application details are at `/openai/codex` and `/anthropic/claude-code`.

```sh
curl -fsSL https://downloads.example.internal/openai/codex/install.sh | sh
curl -fsSL https://downloads.example.internal/anthropic/claude-code/install.sh | bash
```

```powershell
irm 'https://downloads.example.internal/openai/codex/install.ps1' | iex
irm 'https://downloads.example.internal/anthropic/claude-code/install.ps1' | iex
```

These commands execute downloaded installers. Inspect scripts first when required by your deployment policy. Codex uses `CODEX_RELEASE` when supplied, otherwise `latest`; for unattended shell installation, put `CODEX_NON_INTERACTIVE=1` before `sh`. The application instructions provide commands generated from the public service address. Old public routes and implicit/default-Codex management APIs are not aliases in this architecture.

Installer downloads start from this service and follow normal HTTP redirects; application runtime/API traffic is not redirected. Validate Windows/macOS installation and the production upstream chain before rollout, and apply enterprise egress policy independently.

## Development and license

The Go executable embeds the frontend built by `npm ci && npm run build` in `frontend/`. Run Go tests and the frontend's typecheck/DOM tests before building `./cmd/redapp`. `make check test frontend-test` covers format/vet/race, offline Shell/maintenance and full DOM tests; build the native binary once, then `make runtime-test` covers data, HTTP, instruction, retention, prewarm, taxonomy and exchange CLI in temporary directories. Official Claude network checks and Windows PS7/5.1 are separate gates.

The current changes, provider IDs and API boundaries are in [the 0.8.0 configuration contract](docs/configuration-v0.8.0.md). The [v0.7.0 guide](docs/provider-runtime-v0.7.0.md) remains the historical cache-policy reference. Older versioned operational references describe their respective releases and are not configuration instructions for this architecture.

Original RedApp code is [MIT licensed](LICENSE). [Third-party licenses](third_party/README.md), including upstream installer LICENSE/NOTICE, remain separate.
