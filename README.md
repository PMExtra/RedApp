# RedApp

[Chinese](README.zh-CN.md)

RedApp distributes **HTTP files, Codex CLI and Claude Code** through one service. Administrators manage vendors and applications, choose a provider and upstream base URL, and inspect cache, traffic and service settings.

This branch implements **0.7.1** locally (not published). SQLite schema is **5**. An exact published v0.7.0 schema-4 directory upgrades atomically in place, preserving application identities, configuration, sources, cache and history. Older or unrecognized schemas still require a new empty directory and are refused without modification. Stop the instance and back up its complete data directory before upgrading. See [0.7.1 runtime changes](docs/admin-experience-v0.7.1.md).

## Start the service

No configuration file is required. The native executable accepts `redapp` or `redapp serve`; the image starts `serve` and its health check resolves the same configuration.

```sh
docker build -t redapp:local .
docker run -d --name redapp --read-only \
  -p 127.0.0.1:8080:8080 \
  -v redapp-v07-data:/var/lib/redapp \
  redapp:local
```

Defaults are `:8080`, `/var/lib/redapp`, and no trusted proxies. RedApp accepts any syntactically valid request Host; configure domain and network access policy at the reverse proxy. Use a new empty directory/volume for this schema, including when upgrading from v0.6. Directory permission failures never fall back elsewhere.

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
| App Info (`info`) | None | Details and bilingual plain-text usage instructions; no file routes |
| Hosted Files (`hosted`) | None | Administrator uploads or one-time URL imports; permanent local downloads until manual deletion |
| HTTP Cache (`http-cache`) | Required; HTTP(S), ports and internal sources supported | Ordered path TTL rules; 300-second default when no rule or Cache-Control applies; manual or optional automatic cleanup by fetched time or last access |
| Codex (`codex`) | Defaults to `https://releases.openai.com/codex`; overridable | Verified release metadata/artifacts; channel TTL default 60 seconds; version cleanup |
| Claude Code (`claude-code`) | Defaults to `https://downloads.claude.ai/claude-code-releases`; overridable | Existing signed metadata and digest checks; channel TTL default 60 seconds; version cleanup |

A fresh directory has no vendors or applications; only compiled provider definitions are available. Create your own vendor and application in the administrator UI. Existing entries are retained on upgrade, and restarting never recreates deleted entries. A BaseUrl change creates a separate source epoch; old cache remains available for explicit management. Disabling or deleting an entry stops new public requests and retains stored data. Deletion reserves the ID; it is not a cache purge.

HTTP Cache maps `/<vendor>/<app>/<relative-path>` below its configured BaseUrl. It supports GET/HEAD, validators and single byte ranges, and rejects query strings and path traversal. Cacheable cold downloads are fully spooled before the response starts, including cold range requests. Responses that cannot be shared use a bounded direct stream. Downloaded files are attachments rather than executable pages.

HTTP Cache supports 1–16 ordered mirror URLs with ordered fallback, round robin or random selection. Source edits use a new cache epoch; validators stay bound to the actual source and cross-source retries restart the whole file. Admins can refresh one cached resource or a pattern. Refresh and cleanup previews use server-side pagination, while execution processes the full frozen selection.

HTTP Cache settings support ordered cache rules, a `stale_fallback` switch (default `true`), and optional automatic-cleanup rules. Matching uses decoded application-relative paths with a leading `/`: doublestar globs match the file or a directory ancestor, while Go RE2 expressions match the whole path. The first matching rule wins. An explicit rule's TTL overrides source Cache-Control and Age without an application-default cap; otherwise, valid source `s-maxage`/`max-age` determines freshness with Age/Date deducted. The application TTL is only the fallback when Cache-Control is absent; Cache-Control without a valid lifetime means TTL 0. Expires adds no separate priority. TTL 0 always contacts the source first but may retain a complete body for failure fallback; there is no separate bypass mode.

An explicit path rule may intentionally override source `no-store`/`private` and reuse that representation at the public download endpoint. Existing Set-Cookie, unsupported-Vary and authorization representation boundaries still apply. Actual upstream overrides with TTL greater than 0 produce one warning per fetch; TTL 0 suppresses that warning. On network failure, timeout or upstream 5xx, `stale_fallback=true` serves an available complete expired representation from the same app/source epoch, with no maximum stale age. Each actual fallback records one warning per shared upstream fetch, with no deduplication or rate limit; cache hits do not warn. Setting the switch to `false` returns an error instead. Source revalidation directives do not force extra validation or prohibit fallback, while ETag/Last-Modified conditional validation remains supported. Fallback does not advance timestamps; 404/410 do not trigger it. Finer controls, presentation and observability remain [in issue #2](https://github.com/PMExtra/RedApp/issues/2).

Manual cleanup combines a path pattern, time basis and cutoff in a frozen preview. Automatic cleanup is disabled until rules are saved, then runs every 15 minutes without an immediate startup deletion. It only processes active HTTP Cache applications' current source epochs, scanning at most 1,000 files and retiring at most 100 per application per pass; cursors prevent starvation. A cleanup rule's first path match owns the file even when its age threshold is not yet met. See the [runtime guide](docs/provider-runtime-v0.7.0.md) for rule examples, limits and API details. New source authentication, private CA and signing options are outside this version; normal TLS certificate verification and existing administrator/release protections remain in place.

## Public address and proxy trust

The global public address determines generated download links and installers. Its priority is:

1. The saved administrator override in site settings.
2. The deployment environment variable `REDAPP_PUBLIC_URL`.
3. The current request origin, derived from a valid Host and trusted proxy headers.

Clearing the administrator override restores the environment default, if present. The interface shows the effective address and its source. Only HTTP(S) origins are accepted, with no credentials, subpath, query, or fragment. An invalid nonempty environment value prevents startup. Changing the public address only affects generated links; it preserves request-origin checks, cookie security, and upstream download authorization.

For enterprise access, use an HTTPS reverse proxy. Configure the proxy's CIDRs in `trusted_proxies` and make the proxy overwrite forwarded headers. Headers from untrusted peers are ignored. RedApp validates Host syntax and derives the request origin from the trusted proxy chain; the proxy controls accepted domains. Health checks connect to the configured local listener, independently of the public address.

Site branding, outbound proxy, and public address are global settings. Cache TTL and provider configuration are per application. Every administrator update uses a revision check so stale browser forms cannot silently overwrite another update. Proxy credentials are never returned by read APIs.

## Install applications

Replace `downloads.example.internal` with your service address. Application details are at `/openai/codex` and `/anthropic/claude-code`.

```sh
curl -fsSL https://downloads.example.internal/openai/codex/install.sh | sh
curl -fsSL https://downloads.example.internal/anthropic/claude-code/install.sh | sh
```

```powershell
irm 'https://downloads.example.internal/openai/codex/install.ps1' | iex
irm 'https://downloads.example.internal/anthropic/claude-code/install.ps1' | iex
```

These commands execute downloaded installers. Inspect scripts first when required by your deployment policy. Codex uses `CODEX_RELEASE` when supplied, otherwise `latest`; for unattended shell installation, put `CODEX_NON_INTERACTIVE=1` before `sh`. The application page provides version-specific commands. Old public routes and implicit/default-Codex management APIs are not aliases in this architecture.

Installer downloads stay on the service; application runtime/API traffic is not redirected. Validate Windows/macOS installation and the production upstream chain before rollout, and apply enterprise egress policy independently.

## Development and license

The Go executable embeds the frontend built by `npm ci && npm run build` in `frontend/`. Run Go tests and the frontend's typecheck/DOM tests before building `./cmd/redapp`. CLI smoke checks in `scripts/test-data-cli.py` and `scripts/test-http-cli.py` use `bin/redapp` and isolated temporary data.

The current changes, provider IDs and API boundaries are in [the 0.7.1 runtime guide](docs/admin-experience-v0.7.1.md). The [v0.7.0 guide](docs/provider-runtime-v0.7.0.md) remains the historical cache-policy reference. Older versioned operational references describe their respective releases and are not configuration instructions for this architecture.

Original RedApp code is [MIT licensed](LICENSE). [Third-party licenses](third_party/README.md), including upstream installer LICENSE/NOTICE, remain separate.
