# RedApp

[Chinese](README.zh-CN.md)

RedApp distributes **Codex CLI and Claude Code** through an internal download service. It verifies application metadata, caches downloads on demand, and provides an administrator interface for cache, traffic, and service settings.

Version 0.6 uses canonical application identities: `openai/codex` and `anthropic/claude-code`. Upgrading from versions before 0.6 is a **breaking upgrade requiring a new, empty data directory**. Configuration, cache, and history from older releases are not imported; keep the old directory as an archive. Startup rejects an old or unrecognized directory without upgrading or deleting it. Existing 0.6 data directories can be reopened normally. See the [v0.6.0 upgrade notes](docs/multi-application-v0.6.0.md) and [current release notes](docs/releases.md) before upgrading.

## Start the service

No configuration file is required. The native executable accepts `redapp` or `redapp serve`; the image starts `serve` and its health check resolves the same configuration.

```sh
docker run -d --name redapp --read-only \
  -p 127.0.0.1:8080:8080 \
  -v redapp-v06-data:/var/lib/redapp \
  ghcr.io/pmextra/redapp:0.6.2
```

Defaults are `:8080`, `/var/lib/redapp`, and no trusted proxies. The unreleased source accepts any syntactically valid request Host; configure domain and network access policy at the reverse proxy. New deployments need a new empty data directory/volume; existing v0.6 data can be reused. Directory permission failures never fall back elsewhere.

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

For an optional container YAML file, add `--mount type=bind,src=/absolute/config.yaml,dst=/etc/redapp/config.yaml,readonly` to `docker run`. For another YAML/JSON path, mount that file and set `REDAPP_CONFIG` to its container path. If instead you override the container command with `--config` or deployment flags, supply equivalent flags in `--health-cmd`; Docker does not forward CMD arguments to HEALTHCHECK. See the [deployment field/env/CLI table](docs/operations.md#启动配置) for all supported options and limits. Upstream origins and trust roots remain compiled, reviewed definitions.

Open **http://localhost:8080/** for the catalog or **http://localhost:8080/admin/overview** for administration. The first-start logs contain the initial admin password; protect those logs and change the password after signing in. Containers run as UID/GID 65532; ensure bind-mounted data directories are writable by that identity.

Unreleased source builds accept capacity strings such as `REDAPP_MAX_ARTIFACT_BYTES=4GiB` through environment, CLI and YAML/JSON configuration, alongside integer byte counts. This is a **per-file** limit, not total cache capacity: `4GB`/`4gb` means 4,000,000,000 bytes, while `4GiB` means 4,294,967,296 bytes. The default remains 4 GiB; the accepted range is 1 byte to 1 TiB. Published v0.6.2 requires integer bytes; see [release notes](docs/releases.md).

The source examples and deployment table describe the unreleased configuration: writer limits use `REDAPP_MAX_WRITERS`, `download_limits.max_writers`, or `--max-writers`; reader names remain unchanged. Removed Host and writer options have no aliases. For published v0.6.2, use its [tagged examples](https://github.com/PMExtra/RedApp/tree/v0.6.2/config).

## Public address and proxy trust

The global public address determines generated download links and installers. Its priority is:

1. The saved administrator override in site settings.
2. The deployment environment variable `REDAPP_PUBLIC_URL`.
3. The current request origin, derived from a valid Host and trusted proxy headers.

Clearing the administrator override restores the environment default, if present. The interface shows the effective address and its source. Only HTTP(S) origins are accepted, with no credentials, subpath, query, or fragment. An invalid nonempty environment value prevents startup. Changing the public address only affects generated links; it preserves request-origin checks, cookie security, and upstream download authorization.

For enterprise access, use an HTTPS reverse proxy. Configure the proxy's CIDRs in `trusted_proxies` and make the proxy overwrite forwarded headers. Headers from untrusted peers are ignored. RedApp validates Host syntax and derives the request origin from the trusted proxy chain; the proxy controls accepted domains. Health checks connect to the configured local listener, independently of the public address.

Site branding, outbound proxy, and public address are global settings. Channel cache TTL is per application. Every administrator update uses a revision check so stale browser forms cannot silently overwrite another update. Proxy credentials are never returned by read APIs.

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

The implementation plan is in [the architecture document](docs/multi-application-architecture-next.md). Older versioned operational references describe their respective releases and are not configuration instructions for this development architecture.

Original RedApp code is [MIT licensed](LICENSE). [Third-party licenses](third_party/README.md), including upstream installer LICENSE/NOTICE, remain separate.
