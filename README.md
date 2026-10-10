# RedApp

[简体中文](README.zh-CN.md)

RedApp is a self-hosted redistribution service for software downloads. It serves HTTP files, Codex CLI and Claude Code from one address inside your network, caches upstream files on local disk, and generates install commands that point at your own server. Administrators manage vendors and applications in a web console; users browse a public catalog and run the install commands.

## Features

- Five providers: app information pages, hosted uploads, a general HTTP cache, and verified Codex and Claude Code releases.
- HTTP cache with ordered path rules, stale fallback, 1–16 mirror sources, refresh, and manual or automatic cleanup.
- Codex and Claude Code release metadata and binaries checked against upstream digests and signatures.
- Official installers with minimal patches that download from your RedApp server.
- Bilingual (English and Simplified Chinese) catalog, application pages and usage instructions.
- Built-in presets with per-field overrides, configuration export/import and application copy.
- Outbound proxy per scope (global, vendor, application), prewarming and version retention.
- Metric history for disk, traffic, requests and resources.

## Quick start

Docker (read-only root file system, data in a named volume):

```sh
docker build -t redapp:local .
docker run -d --name redapp --read-only \
  -p 127.0.0.1:8080:8080 \
  -v redapp-data:/var/lib/redapp \
  redapp:local
```

Native binary (Linux, needs Go, Node.js and a C toolchain):

```sh
make build
./bin/redapp serve --data /absolute/writable/redapp-data
```

No configuration file is required. Environment variables are enough:

```sh
REDAPP_DATA=/absolute/writable/redapp-data \
REDAPP_LISTEN=127.0.0.1:8080 \
REDAPP_PUBLIC_URL=https://downloads.example.internal \
  redapp
```

## First login

1. Read the service log after the first start. It prints `Initial admin password: ...` exactly once.
2. Open `http://localhost:8080/admin/overview` and sign in as the administrator.
3. Change the password, then protect or rotate the log that contains the initial password.
4. Built-in vendors and applications start disabled. Enable a vendor and its application to publish them.

The public catalog is at `http://localhost:8080/`.

## Install clients

Replace `downloads.example.internal` with your public address. Each application page shows the same commands for your server.

```sh
curl -fsSL https://downloads.example.internal/openai/codex/install.sh | sh
curl -fsSL https://downloads.example.internal/anthropic/claude-code/install.sh | bash
```

```powershell
irm 'https://downloads.example.internal/openai/codex/install.ps1' | iex
irm 'https://downloads.example.internal/anthropic/claude-code/install.ps1' | iex
```

These commands run downloaded scripts. Review them first if your policy requires it.

## Documentation

- [Configuration](docs/guide/configuration.md): deployment settings, public address, proxies, presets, export and import.
- [Operations](docs/guide/operations.md): Docker, data directory, reverse proxy, health checks, backups, troubleshooting.
- [Providers](docs/guide/providers.md): provider types, HTTP cache rules, release distribution, usage instructions.
- [Security](docs/guide/security.md): trust model, authentication, sandboxing, upstream verification.
- [Metrics](docs/guide/metrics.md): metric names, sampling, retention and gaps.
- [Development](docs/dev/development.md): building, testing and contributing (Chinese).

## License

RedApp is [MIT licensed](LICENSE). Third-party components and upstream installers keep their own licenses; see [third_party](third_party/README.md).
