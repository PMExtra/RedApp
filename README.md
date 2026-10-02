# RedApp

[Chinese](README.zh-CN.md)

RedApp helps IT administrators distribute **Codex CLI** through an internal download endpoint. It caches downloads on demand and provides an admin page for managing cached versions, disk usage, and traffic.

## Quick start

Use a Linux/amd64 or Linux/arm64 (aarch64) host that can pull the image, or [build from source](docs/README.md#build-from-source). Anonymous GHCR access has not been verified. Current images use `/var/lib/redapp`; the older v0.1.0 image uses `/data` and must not be used with the volume path below.

```sh
docker run -d --name redapp --read-only \
  -p 127.0.0.1:8080:8080 -v redapp-data:/var/lib/redapp \
  -e REDAPP_PUBLIC_URL=http://localhost:8080 \
  ghcr.io/pmextra/redapp:latest
docker logs redapp
```

Open **http://localhost:8080/** for the application catalog and Codex installation instructions. Administrators sign in at **http://localhost:8080/admin/**. The initial admin password appears in the first-start logs only; change it after signing in and protect those logs. The named volume keeps data across container replacements.

For enterprise access, put RedApp behind an HTTPS reverse proxy. `REDAPP_PUBLIC_URL` is optional: an explicit value fixes the external origin; otherwise RedApp derives it from each request. Configure trusted proxy CIDRs before using forwarded headers and restrict access through your proxy or network policy. See [deployment and configuration](docs/operations.md).

Docker selects Linux/amd64 or Linux/arm64 (aarch64) automatically. Use a version tag such as `v0.4.0` when you need a fixed server version.

## Install on clients

Replace `codex.example.internal` with your service address:

```sh
curl -fsSL https://codex.example.internal/install.sh | sh
```

```powershell
irm 'https://codex.example.internal/install.ps1' | iex
```

These commands download and execute the installer immediately, using `CODEX_RELEASE` if set or `latest` otherwise. Version-pinned commands are in the installation details. For unattended shell installation, put `CODEX_NON_INTERACTIVE=1` before `sh` in the pipeline. To review the script first, see [client installation details](docs/README.md#review-before-installing).

## Before rollout

- Run one server instance per local data directory; shared network filesystems and public URL subpaths are unsupported.
- Installer downloads stay on your service. Codex runtime/API traffic is not redirected, and suppressing the installer update marker does not disable every CLI update check. Apply your enterprise egress policy.
- Windows/macOS installation and the production upstream download chain still need rollout validation.

See the [documentation index](docs/README.md) for advanced configuration, backups, design, validation, and developer instructions. Detailed reference documents are currently in Chinese.

## License

Original RedApp code is [MIT licensed](LICENSE). [Third-party licenses](third_party/README.md), including the Codex installer LICENSE/NOTICE, remain separate.
