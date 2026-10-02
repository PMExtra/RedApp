# RedApp

[Chinese](README.zh-CN.md)

RedApp distributes **Codex CLI and Claude Code** through an internal download service. It verifies application metadata, caches downloads on demand, and provides an administrator interface for cache, traffic, and service settings.

This development architecture uses canonical application identities: `openai/codex` and `anthropic/claude-code`. It requires a **new, empty data directory**. Configuration, cache, and history from older releases are not imported; keep the old directory as an archive. Startup rejects an old or unrecognized directory without upgrading or deleting it. Existing new-format directories can be reopened normally.

## Start the service

Copy [config/example.json](config/example.json) to a deployment file and edit `allowed_hosts` to list the exact public authorities (including nondefault ports). The example permits `localhost:8080`; it is not a wildcard. The data directory must be an absolute path.

```sh
redapp config validate --config /etc/redapp/config.json
redapp serve --config /etc/redapp/config.json
redapp healthcheck --config /etc/redapp/config.json
```

Configuration validation does not open or create the data directory. `schema_version`, `data_dir`, and a nonempty `allowed_hosts` are required. Omitted listen and download limits use the documented example defaults. Unknown fields, duplicate JSON keys, null values, invalid CIDRs, and out-of-range values are rejected. Writers support 1–1024, readers 1–65536, and maximum artifact size 1 byte–1 TiB. These limits are shared across all applications.

For an image built from this revision, mount the configuration read-only and use a new named volume:

```sh
docker run -d --name redapp --read-only \
  -p 127.0.0.1:8080:8080 \
  -v redapp-next-data:/var/lib/redapp \
  -v /etc/redapp/config.json:/etc/redapp/config.json:ro \
  redapp:local
```

The image starts `serve --config /etc/redapp/config.json`; its health check reads the same file. One process owns each local data directory. Shared network filesystems and URL subpath deployment are unsupported. Docker and native processes no longer accept the old per-field CLI flags or `REDAPP_DATA`, `REDAPP_LISTEN`, and upstream override variables. Upstream origins and trust roots are reviewed, compiled application definitions.

Open **http://localhost:8080/** for the catalog or **http://localhost:8080/admin/overview** for administration. The first-start logs contain the initial admin password; protect those logs and change the password after signing in. Containers run as UID/GID 65532; ensure bind-mounted data directories are writable by that identity.

## Public address and proxy trust

The global public address determines generated download links and installers. Its priority is:

1. The saved administrator override in site settings.
2. The deployment environment variable `REDAPP_PUBLIC_URL`.
3. The current request origin, verified against `allowed_hosts` and `trusted_proxies`.

Clearing the administrator override restores the environment default, if present. The interface shows the effective address and its source. Only HTTP(S) origins are accepted, with no credentials, subpath, query, or fragment. An invalid nonempty environment value prevents startup. Changing the public address does not authorize another incoming Host, change cookie security, or alter upstream download origins.

For enterprise access, use an HTTPS reverse proxy. Configure the proxy's CIDRs in `trusted_proxies` and make the proxy overwrite forwarded headers. Headers from untrusted peers are ignored. The effective forwarded authority must still be listed in `allowed_hosts`. Health checks connect to the configured listener with an allowed Host, independently of the public address.

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
