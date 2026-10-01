# RedApp

[Chinese](README.zh-CN.md)

RedApp helps IT administrators distribute **Codex CLI** through an internal download endpoint. It caches downloads on demand and provides an admin page for managing cached versions, disk usage, and traffic.

## Quick start

Use a Linux/amd64 or Linux/arm64 (aarch64) host that can pull the v0.2.1 image, or [build from source](docs/README.md#build-from-source). Anonymous GHCR access has not been verified. v0.2.1 uses `/var/lib/redapp`; the older v0.1.0 image uses `/data` and must not be used with the volume path below.

```sh
docker run -d --name redapp --read-only \
  -p 127.0.0.1:8080:8080 -v redapp-data:/var/lib/redapp \
  -e REDAPP_PUBLIC_URL=http://localhost:8080 \
  ghcr.io/pmextra/redapp:v0.2.1
docker logs redapp
```

Open **http://localhost:8080/admin/**. The initial admin password appears in the first-start logs only; change it after signing in and protect those logs. The named volume keeps data across container replacements.

For enterprise access, put RedApp behind an HTTPS reverse proxy and set `REDAPP_PUBLIC_URL` to its external origin. Preserve the configured Host and restrict download access through your proxy or network policy. See [deployment and configuration](docs/operations.md).

v0.2.1 targets Linux/amd64 and Linux/arm64 (aarch64). Docker selects the host architecture automatically; v0.2.0 remains amd64-only.

## Install on clients

Replace `codex.example.internal` with your service address:

```sh
curl -fsS https://codex.example.internal/install.sh | sh -s -- --release 0.159.2
```

```powershell
& ([scriptblock]::Create((Invoke-WebRequest -UseBasicParsing 'https://codex.example.internal/install.ps1' -ErrorAction Stop).Content)) -Release 0.159.2
```

These commands download and execute the installer immediately. For unattended shell installation, put `CODEX_NON_INTERACTIVE=1` before `sh` in the pipeline. To review the script first, see [client installation details](docs/README.md#review-before-installing).

## Before rollout

- Run one server instance per local data directory; shared network filesystems and public URL subpaths are unsupported.
- Installer downloads stay on your service. Codex runtime/API traffic is not redirected, and suppressing the installer update marker does not disable every CLI update check. Apply your enterprise egress policy.
- Windows/macOS installation and the production upstream download chain still need rollout validation.

See the [documentation index](docs/README.md) for advanced configuration, backups, design, validation, and developer instructions. Detailed reference documents are currently in Chinese.

## License

Original RedApp code is [MIT licensed](LICENSE). [Third-party licenses](third_party/README.md), including the Codex installer LICENSE/NOTICE, remain separate.
