# RedApp documentation

[English quick start](../README.md) · [Chinese quick start](../README.zh-CN.md)

Use the quick starts to run the service and install Codex on clients. The references below cover advanced operation and development; the existing detailed reference documents are in Chinese.

## Administrator references

- [Release notes](releases.md): upgrade guidance and changes by version.
- [Operations](operations.md): CLI/environment configuration, latest TTL, reverse proxy and trusted headers, access controls, persistent volumes, backups, recovery, health checks, cleanup, and metrics.
- [Global metric history](metrics-history.md): fixed metric catalog, minute observations, UTC hourly aggregates, retention, gaps, and chart semantics.
- [Validation and limitations](acceptance.md): the acceptance matrix, concurrency/failure tests, crash windows, and remaining platform/upstream verification gates.

`REDAPP_PUBLIC_URL` is optional and defaults to empty. An explicit value must be an origin without a subpath, query, or credentials, and requests must use its exact Host. Otherwise the origin is derived per request; only configured trusted proxies can supply forwarded host/scheme. Host syntax validation does not establish domain trust. For production, use HTTPS at the reverse proxy; download endpoints do not require an admin session, so network access controls remain necessary. See the operations reference for configuration details rather than copying its configuration table here.

## Review before installing

The quick-start commands execute the downloaded installer immediately. Administrators who need to inspect it first can download it from their own RedApp service, review the file, and then execute it. Replace `codex.example.internal` with your service address. Run the execution command only after a successful download and review; do not execute a stale or partial file after a download failure.

Shell download and review:

```sh
curl -fsS https://codex.example.internal/install.sh -o install.sh && less install.sh
```

After review:

```sh
sh install.sh --release 0.159.2
```

PowerShell download and review:

```powershell
Invoke-WebRequest -UseBasicParsing 'https://codex.example.internal/install.ps1' -OutFile install.ps1 -ErrorAction Stop
Get-Content ./install.ps1
```

After a successful download and review:

```powershell
./install.ps1 -Release 0.159.2
```

Both methods preserve the installer's release arguments. Omit `--release 0.159.2` / `-Release 0.159.2` to use `CODEX_RELEASE`, or `latest` when that environment variable is unset. For a version-pinned one-line installation, pass parameters to the shell or PowerShell script block explicitly:

```sh
curl -fsSL https://codex.example.internal/install.sh | sh -s -- --release 0.159.2
```

```powershell
& ([scriptblock]::Create((Invoke-WebRequest -UseBasicParsing 'https://codex.example.internal/install.ps1' -ErrorAction Stop).Content)) -Release 0.159.2
```

The quick-start `irm ... | iex` command is for the default release; do not append `-Release` to `iex`.

Shell confirmation prompts use the controlling terminal (`/dev/tty`) when available, including with piped execution. Without an interactive terminal, optional confirmations are declined without consuming the downloaded script. PowerShell prompts use `Read-Host` when console input and output are not redirected; optional confirmations are declined when they are redirected. To explicitly decline optional confirmations for unattended installation, set `CODEX_NON_INTERACTIVE=1` on the installer process. For piped shell execution:

```sh
curl -fsS https://codex.example.internal/install.sh | CODEX_NON_INTERACTIVE=1 sh -s -- --release 0.159.2
```

For PowerShell, set `$env:CODEX_NON_INTERACTIVE = '1'` before running the chosen install command. Shell pipelines can start executing before the whole script has downloaded, and without `pipefail` a pipeline's exit status may hide a download failure; use the review-first method when complete-download-before-execution is required.

## Build from source

Use Linux/amd64 or Linux/arm64 (aarch64) with Go 1.27.1, Node.js 24.19.0, npm, GCC, static libc development libraries, and Make. Docker builds require Docker and access to the builder image and Go modules.

```sh
git clone https://github.com/PMExtra/RedApp.git
cd RedApp
make build
./bin/redapp --data ./data --public-url http://localhost:8080
```

Open `http://localhost:8080/admin/` and use the first-start password from the process logs. For a locally built container:

```sh
docker build --build-arg VERSION="$(cat VERSION)" \
  --build-arg REVISION="$(git rev-parse HEAD)" -t redapp:local .
docker run -d --name redapp --read-only \
  -p 127.0.0.1:8080:8080 -v redapp-data:/var/lib/redapp \
  -e REDAPP_PUBLIC_URL=http://localhost:8080 redapp:local
```

The current Docker image runs as UID/GID 65532 and needs a writable local data volume at `/var/lib/redapp`. The binary and host services use the same default; `--data` overrides nonempty `REDAPP_DATA`, which overrides that default. Provision the host directory for the service user before startup. There is no environment detection, permission-failure fallback, or automatic migration from `/data`. For local development, explicitly use `--data ./data`, as above. New empty named volumes inherit the prepared directory ownership and mode, so first startup needs no manual permission changes. Existing host bind mounts must already be writable by UID/GID 65532; the nonroot image does not repair their permissions. This follows the build-time filesystem preparation and fixed nonroot user pattern in the official [Loki Dockerfile](https://github.com/grafana/loki/blob/main/cmd/loki/Dockerfile). v0.2.0 uses this path contract; the older v0.1.0 image still uses `/data`. The binary statically links SQLite through CGO, disables SQLite extension loading, and uses Go DNS/user lookup implementations; no separate database service is required.

Docker uses the target-platform `golang:1.27.1-trixie` builder (Debian 13), including its native GCC/libc toolchain for CGO SQLite. Native builds compile directly; nonnative builds require Buildx/QEMU or an appropriate native builder. `CGO_ENABLED=0` is not a supported substitute. CI uses the native `ubuntu-26.04` and `ubuntu-26.04-arm` runners for race/CLI tests, full builds, and real container lifecycle checks. Official CI and publication validate Linux/amd64 and Linux/arm64; ordinary client runs need no `--platform` setting. See [Docker multi-platform builds](https://docs.docker.com/build/building/multi-platform/) and [GitHub runner specifications](https://docs.github.com/en/actions/reference/runners/github-hosted-runners).

## Design and development

- [Design baseline](design.md): requirements, architecture boundaries, lifecycle, version cleanup, security, and acceptance criteria.
- [Upstream contract](upstream-contract.md): pinned official installer sources, metadata/digest rules, platform selection, fallback behavior, and verification boundaries.
- [Installer maintenance](installers-maintenance.md): minimal patch policy, provenance, generated-asset consistency, failure checks, and atomic updater behavior.
- [Third-party licenses](../third_party/README.md): dependency and vendored installer notices; these do not replace the project's [MIT License](../LICENSE).

The server embeds admin assets, enterprise installers, and Codex license materials. Changes to those assets require rebuilding the binary. Keep the fixed-upstream distributor/cache separate from the statically linked Codex module; this version supports one application and one owning process.

From the repository root:

```sh
make check test build
python3 scripts/test-data-cli.py
python3 scripts/test-http-cli.py
sh scripts/test-docker-local.sh
make frontend-test
python3 scripts/update-installers.py --source installers/codex/upstream
```

The updater's default mode checks without modifying published assets. To update from an immutable official commit, use `python3 scripts/update-installers.py --apply`; changing the baseline requires explicitly verified shell/PowerShell SHA256 values and review of patches, licenses, and test differences. Full details and minimal-change rules remain in the installer maintenance reference.

## CI, versions, and container publication

[CI](../.github/workflows/ci.yml) runs on PRs and main pushes: formatting, vet, race, installer/CLI tests, full native Linux/amd64 and Linux/arm64 Docker builds, and container persistence/recovery checks. PRs do not log in to or publish to a registry.

[Publication](../.github/workflows/publish.yml) runs only for this repository's stable `v0.x.y` tags. A tag must match `VERSION`, and its exact commit must have passed main CI. Actions uses its temporary `GITHUB_TOKEN` to publish `ghcr.io/pmextra/redapp` tags `v0.x.y`, `0.x.y`, `0.x`, and `latest`, then pulls the digest and verifies the running container. New releases publish a manifest for Linux/amd64 and Linux/arm64, then verify both digest-selected variants (amd64 natively and arm64 under QEMU); v0.2.0 remains amd64-only. Variant checks pull each child manifest digest to avoid index replacement conflicts in Docker classic image storage. The [published-image verification workflow](../.github/workflows/verify-published.yml) checks an existing version tag on native amd64/arm64 runners when verification code changes, without publishing or moving the tag. The workflow does not change GHCR package visibility or persistent account permissions; anonymous access has not been verified.

```sh
./bin/redapp version
docker run --rm ghcr.io/pmextra/redapp:latest version
```

`make build` embeds `VERSION` and the Git revision. Container build arguments provide the same information; OCI labels include source, version, revision, and the original-code MIT license. For the released v0.1.0 image, full build and digest-pull/runtime verification passed in [CI](https://github.com/PMExtra/RedApp/actions/runs/36788207268) and [publication](https://github.com/PMExtra/RedApp/actions/runs/36788716836). Windows/macOS real-machine installation, real upstream download chains, and bundled artifact license reviews remain separate validation gates.

## Admin frontend development

See [frontend](../frontend/README.md) for Vue components, typed API, locked dependencies, CLI tests and embedded build assets. `make build` compiles the frontend before Go; Node is needed only for development/building, never at runtime.
